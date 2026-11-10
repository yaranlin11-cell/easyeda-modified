package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRebindDispatchErrorAddsLateMutationRecoveryAdvice(t *testing.T) {
	got := rebindDispatchError(context.DeadlineExceeded)
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("timeout cause was lost: %v", got)
	}
	for _, want := range []string{"Do not blindly retry", "do not run PCB import-changes", "exact uniqueId"} {
		if !strings.Contains(got.Error(), want) {
			t.Fatalf("missing recovery advice %q in %v", want, got)
		}
	}
}

func TestRebindDispatchErrorLeavesOrdinaryFailuresUnchanged(t *testing.T) {
	want := errors.New("ordinary failure")
	if got := rebindDispatchError(want); got != want {
		t.Fatalf("ordinary error changed: got %v want %v", got, want)
	}
}

func TestRebindPreparedDeviceRequiresCompletePair(t *testing.T) {
	for _, flag := range []string{"--prepared-device-uuid", "--prepared-device-lib"} {
		var out, errOut bytes.Buffer
		cmd := newSchCmd(&appConfig{}, &out, &errOut)
		cmd.SetArgs([]string{"rebind-footprint", "--id", "original", "--footprint-uuid", "target", flag, "one-half"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "must be supplied together") {
			t.Fatalf("%s: %v", flag, err)
		}
	}
}
