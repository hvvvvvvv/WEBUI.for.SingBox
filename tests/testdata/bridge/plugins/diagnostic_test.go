package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/network"
)

func diagnosticForTest(source string) Plugin {
	p := pluginForTest("diagnostic", source, OnManual)
	p.ManualTarget = ManualDiagnostic
	return p
}

func diagnosticReport(t *testing.T, s *Service, source string) map[string]any {
	t.Helper()
	saveForTest(t, s, diagnosticForTest(source))
	report, _, err := s.RunDiagnostic(context.Background(), "diagnostic")
	if err != nil {
		t.Fatal(err)
	}
	object, ok := report.(map[string]any)
	if !ok {
		t.Fatalf("expected report object, got %#v", report)
	}
	return object
}

func TestDiagnosticFetchRoutesAndDoesNotExposeHostSecrets(t *testing.T) {
	var proxyCalls, directCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("User-Agent") != "router-diagnostic-agent" {
			t.Errorf("unexpected diagnostic headers: %v", r.Header)
		}
		_, _ = io.WriteString(w, "proxy response")
	}))
	defer proxy.Close()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls.Add(1)
		_, _ = io.WriteString(w, "direct response")
	}))
	defer direct.Close()
	s := serviceForTest(t)
	s.SetNetworkOptions(func() network.Options {
		return network.Options{
			Mode: network.ModeCustom, ProxyURL: proxy.URL, UserAgent: "router-diagnostic-agent",
			GitHubToken: "NEVER-EXPOSE-THIS-TOKEN", GitHubMirror: "https://mirror.invalid/{url}",
			CoreProxy: func() (string, error) { return proxy.URL, nil },
		}
	})
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun() {
 if (arguments.length !== 0 || typeof process !== 'undefined' || typeof require !== 'undefined') throw Error('unexpected host capability');
 return {configured: Plugins.fetch('http://api.github.com/diagnostic'), core: Plugins.fetch('http://example.invalid/core',{route:'core'}), direct: Plugins.fetch(%q,{route:'direct'})};
}`, direct.URL))
	if proxyCalls.Load() != 2 || directCalls.Load() != 1 || report["configured"].(map[string]any)["body"] != "proxy response" || report["direct"].(map[string]any)["body"] != "direct response" {
		t.Fatalf("wrong route or response: %#v proxy=%d direct=%d", report, proxyCalls.Load(), directCalls.Load())
	}
	before := jsonForTest(t, s.List())
	if _, _, err := s.RunSubscription(context.Background(), "diagnostic", []map[string]any{{"tag": "input", "type": "direct"}}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("diagnostic entered subscription pipeline: %v", err)
	}
	if jsonForTest(t, s.List()) != before {
		t.Fatal("diagnostic mutated stored state")
	}
	ordinary := pluginForTest("ordinary", `function onRun(p) { if (typeof Plugins.fetch !== 'undefined' || typeof Plugins.measureDownload !== 'undefined') throw Error('network exposed'); return p }`, OnManual)
	saveForTest(t, s, ordinary)
	if _, _, err := s.RunSubscription(context.Background(), ordinary.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RunDiagnostic(context.Background(), ordinary.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ordinary plugin received diagnostic capability: %v", err)
	}
}

func TestDiagnosticRejectsUnsupportedRequestsAndRedactsErrors(t *testing.T) {
	s := serviceForTest(t)
	s.SetNetworkOptions(func() network.Options {
		return network.Options{Mode: network.ModeCustom, ProxyURL: "http://privateuser:privatepass@127.0.0.1:1"}
	})
	for _, invocation := range []string{
		`Plugins.fetch('file:///etc/passwd')`, `Plugins.fetch('http://user:pass@example.com')`,
		`Plugins.fetch('http://example.com',{method:'POST'})`, `Plugins.fetch('http://example.com',{headers:{Authorization:'secret'}})`,
		`Plugins.fetch('http://example.com',{body:'body'})`, `Plugins.fetch('http://example.com',{route:'arbitrary'})`,
		`Plugins.fetch('http://example.com',null)`, `Plugins.fetch('http://example.com',{},'extra')`,
		`Plugins.measureDownload('http://example.com',{max_bytes:8388609})`, `Plugins.measureDownload('http://example.com',{max_bytes:1.5})`,
		`Plugins.measureDownload('http://example.com',{method:'GET'})`,
	} {
		report := diagnosticReport(t, s, "function onRun(){try {"+invocation+"; return {accepted:true}} catch(e){return {code:e.code, message:e.message}}}")
		if report["code"] != "invalid" {
			t.Fatalf("accepted unsafe request %s: %#v", invocation, report)
		}
	}
	report := diagnosticReport(t, s, `function onRun(){try {Plugins.fetch('http://example.com'); return {accepted:true}} catch(e){return {code:e.code, message:e.message}}}`)
	if report["code"] != "network" || strings.Contains(jsonForTest(t, report), "private") {
		t.Fatalf("transport secret exposed or wrong error: %#v", report)
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsServer.Close()
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){try {Plugins.fetch(%q,{route:'direct'}); return {accepted:true}} catch(e){return {code:e.code}}}`, tlsServer.URL))
	if report["code"] != "tls" {
		t.Fatalf("certificate failure misclassified: %#v", report)
	}
}

func TestDiagnosticResponseAndRequestBudgets(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, strings.Repeat("x", MaxDiagnosticBodyBytes+100))
		}
	}))
	defer server.Close()
	s := serviceForTest(t)
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun(){let lengths=[];for(let i=0;i<4;i++){let r=Plugins.fetch(%q);if(!r.truncated)throw Error('missing truncation');lengths.push(r.body.length)};try{Plugins.fetch(%q)}catch(e){return {lengths:lengths,code:e.code,message:e.message}};throw Error('budget missing')}`, server.URL, server.URL))
	if calls.Load() != 4 || report["code"] != "policy" {
		t.Fatalf("body budget ignored: calls=%d %#v", calls.Load(), report)
	}
	for _, value := range report["lengths"].([]any) {
		if value != json.Number("65536") {
			t.Fatalf("wrong truncation size: %v", value)
		}
	}
	calls.Store(0)
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){for(let i=0;i<8;i++){let r=Plugins.fetch(%q,{method:'HEAD'});if(r.body!==''||r.truncated)throw Error('head body')};try{Plugins.fetch(%q,{method:'HEAD'})}catch(e){return {code:e.code}};throw Error('request budget missing')}`, server.URL, server.URL))
	if calls.Load() != 8 || report["code"] != "policy" {
		t.Fatalf("request budget ignored: calls=%d %#v", calls.Load(), report)
	}
}

func TestDiagnosticRedirectsSameOriginAndShareRequestBudget(t *testing.T) {
	var escaped, calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/cross":
			http.Redirect(w, r, other.URL, http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/scheme":
			http.Redirect(w, r, "https://"+r.Host+"/ok", http.StatusFound)
		case "/same":
			http.Redirect(w, r, "/ok", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	defer server.Close()
	s := serviceForTest(t)
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun(){let success=Plugins.fetch(%q);let cross,loop,scheme;try{Plugins.fetch(%q)}catch(e){cross=e.code};try{Plugins.fetch(%q)}catch(e){scheme=e.code};try{Plugins.fetch(%q)}catch(e){loop=e.code};return {body:success.body,cross,loop,scheme}}`, server.URL+"/same", server.URL+"/cross", server.URL+"/scheme", server.URL+"/loop"))
	if report["body"] != "ok" || report["cross"] != "policy" || report["scheme"] != "policy" || report["loop"] != "policy" || escaped.Load() != 0 || calls.Load() != 8 {
		t.Fatalf("redirect policy failed: %#v escaped=%d calls=%d", report, escaped.Load(), calls.Load())
	}
	calls.Store(0)
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){for(let i=0;i<4;i++)Plugins.fetch(%q);try{Plugins.fetch(%q)}catch(e){return {code:e.code}}}`, server.URL+"/same", server.URL+"/same"))
	if calls.Load() != 8 || report["code"] != "policy" {
		t.Fatalf("redirect requests not counted: %d %#v", calls.Load(), report)
	}
}

func TestDiagnosticIncompleteResponsesStillConsumeBodyBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(MaxDiagnosticBodyBytes*2))
		_, _ = io.WriteString(w, strings.Repeat("x", MaxDiagnosticBodyBytes))
	}))
	defer server.Close()
	s := serviceForTest(t)
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun(){let failures=[];for(let i=0;i<5;i++){try{Plugins.fetch(%q)}catch(e){failures.push(e.code)}};return {failures}}`, server.URL))
	if calls.Load() != 4 || jsonForTest(t, report["failures"]) != `["network","network","network","network","policy"]` {
		t.Fatalf("failed response bypassed body budget: %d %#v", calls.Load(), report)
	}
}

func TestDiagnosticMeasurementRejectsErrorPagesAndReportsTimeoutSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", "100000")
		_, _ = io.WriteString(w, strings.Repeat("x", 1024))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	s := serviceForTest(t)
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun(){try{Plugins.measureDownload(%q)}catch(e){return {code:e.code,message:e.message}}}`, server.URL+"/error"))
	if report["code"] != "policy" || !strings.Contains(report["message"].(string), "HTTP 403") {
		t.Fatalf("error page was measured: %#v", report)
	}
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){return Plugins.fetch(%q)}`, server.URL+"/error"))
	if report["status"] != json.Number("403") {
		t.Fatalf("fetch lost HTTP error status: %#v", report)
	}
	started := time.Now()
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){return Plugins.measureDownload(%q)}`, server.URL+"/slow"))
	if report["bytes"] != json.Number("1024") || report["incomplete"] != true || report["truncated"] != false || time.Since(started) > 8*time.Second {
		t.Fatalf("timeout sample missing or unbounded: %#v elapsed=%v", report, time.Since(started))
	}
}

func TestDiagnosticDownloadMeasurementIsBoundedAndDoesNotReturnBody(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("measurement enabled automatic decompression")
		}
		_, _ = io.WriteString(w, strings.Repeat("x", 2<<20))
	}))
	defer server.Close()
	s := serviceForTest(t)
	report := diagnosticReport(t, s, fmt.Sprintf(`function onRun(){let measurement=Plugins.measureDownload(%q);try{Plugins.measureDownload(%q)}catch(e){return {measurement,second:e.code}}}`, server.URL, server.URL))
	measurement := report["measurement"].(map[string]any)
	if calls.Load() != 1 || measurement["bytes"] != json.Number("1048576") || measurement["truncated"] != true || measurement["incomplete"] != false || measurement["body"] != nil || report["second"] != "policy" {
		t.Fatalf("measurement not bounded: %#v calls=%d", report, calls.Load())
	}
	rate, err := measurement["bytes_per_second"].(json.Number).Float64()
	if err != nil || rate <= 0 {
		t.Fatalf("invalid measured rate: %#v", measurement)
	}
	report = diagnosticReport(t, s, fmt.Sprintf(`function onRun(){return Plugins.measureDownload(%q,{max_bytes:1024,route:'direct'})}`, server.URL))
	if report["bytes"] != json.Number("1024") || report["truncated"] != true {
		t.Fatalf("explicit byte limit ignored: %#v", report)
	}
}

func TestDiagnosticCancellationIncludesQueueVMAndNetwork(t *testing.T) {
	for _, source := range []string{`function onRun(){while(true){}}`, `function onRun(){return {toJSON(){while(true){}}}}`, `function onRun(){Plugins.fetch('http://example.com',{get route(){while(true){}}})}`} {
		s := serviceForTest(t)
		saveForTest(t, s, diagnosticForTest(source))
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		_, _, err := s.RunDiagnostic(ctx, "diagnostic")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("VM cancellation failed: %v", err)
		}
	}
	s := serviceForTest(t)
	saveForTest(t, s, diagnosticForTest(`function onRun(){return {ok:true}}`))
	s.runSlot <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, _, err := s.RunDiagnostic(ctx, "diagnostic")
	cancel()
	<-s.runSlot
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queue did not honor cancellation: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	saveForTest(t, s, diagnosticForTest(fmt.Sprintf(`function onRun(){return Plugins.fetch(%q)}`, server.URL)))
	ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
	_, _, err = s.RunDiagnostic(ctx, "diagnostic")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("network did not honor cancellation: %v", err)
	}
	// Cancellation releases the shared execution slot for the next diagnostic.
	if report := diagnosticReport(t, s, `function onRun(){return {ok:true}}`); report["ok"] != true {
		t.Fatal(report)
	}
}

func TestDiagnosticReportAndDisabledBoundaries(t *testing.T) {
	s := serviceForTest(t)
	p := diagnosticForTest(`function onRun(){console.log('hello')}`)
	saveForTest(t, s, p)
	report, logs, err := s.RunDiagnostic(context.Background(), p.ID)
	if err != nil || report != nil || len(logs) != 1 || logs[0] != "hello" {
		t.Fatalf("logs-only diagnostic failed: %#v %#v %v", report, logs, err)
	}
	p.Disabled = true
	saveForTest(t, s, p)
	if _, _, err := s.RunDiagnostic(context.Background(), p.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disabled diagnostic ran: %v", err)
	}
	p.Disabled = false
	p.Source = fmt.Sprintf(`function onRun(){return 'x'.repeat(%d)}`, MaxDiagnosticReportBytes)
	saveForTest(t, s, p)
	before := jsonForTest(t, s.List())
	if _, _, err := s.RunDiagnostic(context.Background(), p.ID); err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("oversized report accepted: %v", err)
	}
	if jsonForTest(t, s.List()) != before {
		t.Fatal("diagnostic changed stored state")
	}
	p.Triggers = []string{OnManual, OnGenerate}
	if err := ValidatePlugin(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("automatic diagnostic trigger accepted: %v", err)
	}
}
