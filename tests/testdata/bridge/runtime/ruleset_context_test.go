package runtime

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	appv1 "guiforcores/gen/app/v1"
)

type contextRuleSetDecompiler struct {
	rulesetDecompilerStub
	called bool
}

func (d *contextRuleSetDecompiler) DecompileRuleSetContext(ctx context.Context, _ string) (string, error) {
	d.called = true
	return "", ctx.Err()
}

func TestRuleSetPreviewPassesRequestCancellationToCore(t *testing.T) {
	withTempBasePath(t)
	decompiler := &contextRuleSetDecompiler{}
	service := newTestRuntimeService(decompiler)
	_, err := putRuleSetForTest(service, `{"id":"binary","tag":"Binary","type":"Http","format":"binary","url":"https://example.invalid/rules.srs"}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.GetRuleSetContent(ctx, connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "binary"}))
	if !decompiler.called || connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("preview did not propagate cancellation: called=%v err=%v", decompiler.called, err)
	}
}
