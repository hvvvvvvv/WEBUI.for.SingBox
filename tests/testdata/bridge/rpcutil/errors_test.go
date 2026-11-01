package rpcutil

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
)

func TestAsConnectErrorPreservesCancellation(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code connect.Code
	}{
		{context.Canceled, connect.CodeCanceled},
		{fmt.Errorf("generate profile: %w", context.DeadlineExceeded), connect.CodeDeadlineExceeded},
		{InvalidArgumentError{Message: "bad profile"}, connect.CodeInvalidArgument},
		{errors.New("unexpected failure"), connect.CodeInternal},
	} {
		if got := AsConnectError(tc.err); connect.CodeOf(got) != tc.code || !errors.Is(got, tc.err) {
			t.Fatalf("%v became %v, want %s and original cause", tc.err, got, tc.code)
		}
	}
	if AsConnectError(nil) != nil {
		t.Fatal("nil became an error")
	}
	existing := connect.NewError(connect.CodeUnavailable, context.Canceled)
	existing.Meta().Set("Retry-After", "3")
	if AsConnectError(existing) != existing {
		t.Fatal("existing transport status/metadata was replaced")
	}
}
