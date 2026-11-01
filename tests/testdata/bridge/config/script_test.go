package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestLegacyConfigScriptKeepsReturnAndMutationStyles(t *testing.T) {
	for _, source := range []string{
		`function onGenerate(c) { c.route.final = 'proxy'; return c }`,
		`function onGenerate(c) { c.route.final = 'proxy' }`,
		`function onGenerate(c) { c.route.final = 'proxy'; return null }`,
		`function onGenerate(c) { return {route:{final:'proxy'}} }`,
		`async function onGenerate(c) { c.route.final = 'proxy'; return c }`,
		`async function onGenerate(c) { c.route.final = 'proxy' }`,
		`JSON.stringify = () => 'wrong'; function onGenerate(c) { c.route.final = 'proxy'; return c }`,
	} {
		input := map[string]any{"route": map[string]any{"final": "direct"}}
		got, err := applyScript(context.Background(), input, &profilev1.Script{Code: source})
		if err != nil || got["route"].(map[string]any)["final"] != "proxy" {
			t.Fatalf("script %s: result=%v error=%v", source, got, err)
		}
		if input["route"].(map[string]any)["final"] != "direct" {
			t.Fatal("script modified caller-owned configuration")
		}
	}
}

func TestLegacyConfigScriptInterruptsAllUserCode(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"top level", `while(true) {}`},
		{"hook", `function onGenerate(c) { while(true) {} }`},
		{"getter", `function onGenerate(c) { return {get value() { while(true) {} }} }`},
		{"toJSON", `function onGenerate(c) { return {toJSON() { while(true) {} }} }`},
		{"exception string", `function onGenerate(c) { throw {toString() { while(true) {} }} }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := applyScript(ctx, map[string]any{}, &profilev1.Script{Code: tc.source})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected caller deadline, got %v", err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("script did not respect caller deadline")
			}
		})
	}
}

func TestLegacyConfigScriptRejectsCyclesAndRecursion(t *testing.T) {
	for _, source := range []string{
		`function onGenerate(c) { c.self=c; return c }`,
		`function recurse() { return recurse() } function onGenerate(c) { return recurse() }`,
		`function onGenerate(c) { throw {toString() { throw Error('nested failure') }} }`,
	} {
		if _, err := applyScript(context.Background(), map[string]any{}, &profilev1.Script{Code: source}); err == nil {
			t.Fatalf("unsafe result was accepted: %s", source)
		}
	}
}

func TestLegacyConfigScriptFailurePreservesGeneratedFile(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, "test")
	path := paths.Resolve(CoreConfigFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("previous configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := service.GenerateConfigFile(ctx, connect.NewRequest(&kernelv1.GenerateConfigFileRequest{
		Profile: &profilev1.Profile{Script: &profilev1.Script{Code: `function onGenerate(c) { while(true) {} }`}},
		Options: &kernelv1.GenerateConfigOptions{EnableScriptProcessing: true},
	}))
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("wrong RPC error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "previous configuration" {
		t.Fatalf("failed script replaced file: %s (%v)", data, err)
	}
	ctx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, err = applyScript(ctx, map[string]any{}, &profilev1.Script{Code: `throw Error('must not execute')`})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "must not execute") {
		t.Fatalf("canceled script executed: %v", err)
	}
}
