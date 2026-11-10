package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFillCreationPathsRequireVerifiedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, result string
		failure      bool
		primitiveID  string
	}{
		{"missing-verification", `{"primitiveId":"kept-fill","source":[0,0,"L",20,0,20,20,0,20,0,0]}`, true, "kept-fill"},
		{"unverified-readback", `{"primitiveId":"kept-fill","verified":false,"lineWidth":0.2,"differences":["geometry"]}`, true, "kept-fill"},
		{"partial-despite-verified", `{"primitiveId":"kept-fill","partial":true,"verified":true,"lineWidth":0.2}`, true, "kept-fill"},
		{"readback-unavailable", `{"primitiveId":"kept-fill","partial":true,"verified":false,"error":"Fill inventory unavailable after creation."}`, true, "kept-fill"},
		{"missing-identity", `{"verified":true,"partial":false}`, true, ""},
		{"verified", `{"primitiveId":"kept-fill","verified":true,"lineWidth":0.2}`, false, "kept-fill"},
		{"verified-not-partial", `{"primitiveId":"kept-fill","verified":true,"partial":false,"lineWidth":0.2}`, false, "kept-fill"},
	} {
		for _, path := range []string{"dispatch", "request", "apply"} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				response := `{"ok":true,"result":` + tc.result + `}`
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return response })
				defer cleanup()
				var out, stderr bytes.Buffer
				var err error
				switch path {
				case "dispatch":
					err = dispatch(cfg, "pcb.fill.create", "w1", map[string]any{}, &out, &stderr)
					if strings.TrimSpace(out.String()) != response {
						t.Fatalf("lost original fill reply: %s", out.String())
					}
				case "request":
					var res *actionResult
					res, err = requestAction(cfg, "pcb.fill.create", "w1", map[string]any{})
					if res == nil {
						t.Fatal("lost partial fill response")
					}
					assertFillResultEvidence(t, res.Result, tc.result)
				case "apply":
					r := applyRunner{cfg: cfg, window: "w1"}
					var result any
					result, err = r.runAction("pcb.fill.create", map[string]any{}, time.Second)
					assertFillResultEvidence(t, result, tc.result)
				}
				if (err != nil) != tc.failure {
					t.Fatalf("err=%v; failure=%v", err, tc.failure)
				}
				if err != nil {
					var boundaryErr *unverifiedPourBoundaryError
					if !errors.As(err, &boundaryErr) || boundaryErr.action != "pcb.fill.create" || boundaryErr.primitiveID != tc.primitiveID {
						t.Fatalf("lost fill failure action/identity: %v", err)
					}
					if !strings.Contains(err.Error(), "pcb.fill.create") || (tc.primitiveID != "" && !strings.Contains(err.Error(), tc.primitiveID)) {
						t.Fatalf("fill failure must identify the retained boundary: %v", err)
					}
				}
				if len(calls.snapshot()) != 1 {
					t.Fatalf("unexpected fill replay: %+v", calls.snapshot())
				}
			})
		}
	}
}

func assertFillResultEvidence(t *testing.T, actual any, expectedJSON string) {
	t.Helper()
	var expected any
	if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("lost fill readback evidence: got %s want %s", gotJSON, expectedJSON)
	}
}

func TestFillApplyCannotVerifyRetryOrContinuePastUnverifiedBoundary(t *testing.T) {
	for _, entry := range []string{"action", "pcb fill create"} {
		for _, mode := range []string{"default", "verify", "retry", "continue", "default-continue", "prompt"} {
			for _, result := range []string{
				`{"primitiveId":"kept-fill","verified":false,"lineWidth":0.2,"differences":["fillMode"]}`,
				`{"primitiveId":"kept-fill","verified":true,"partial":true}`,
				`{"primitiveId":"kept-fill","verified":false,"partial":true,"error":"readback unavailable"}`,
			} {
				t.Run(entry+"/"+mode+"/"+result, func(t *testing.T) {
					creates := 0
					cfg, state, cleanup := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
						if call.Action == "pcb.fill.create" {
							creates++
							if creates == 1 {
								return `{"ok":true,"result":` + result + `}`
							}
							return `{"ok":true,"result":{"primitiveId":"duplicate-fill","verified":true}}`
						}
						return `{"ok":true,"result":{"fills":[{"primitiveId":"kept-fill"}],"verified":true}}`
					})
					defer cleanup()
					step := playbookStep{ID: "bad-fill", Action: "pcb.fill.create", Payload: map[string]any{
						"net": "GND", "points": [][]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}},
					}}
					if entry != "action" {
						step.Action, step.Run = "", entry
						step.Flags = map[string]any{"net": "GND", "points": "[[0,0],[20,0],[20,20],[0,20]]"}
					}
					pb := &playbook{Version: 1}
					switch mode {
					case "verify":
						step.Verify = &verifyBlock{Action: "pcb.fill.list"}
					case "retry":
						retries := 2
						step.Retry = &retries
					case "continue", "prompt":
						step.OnFail = mode
					case "default-continue":
						cont := true
						pb.Defaults.ContinueOnError = &cont
					}
					pb.Steps = []playbookStep{step, {ID: "dependent", Action: "pcb.line.create", Payload: map[string]any{}}}
					var out, stderr bytes.Buffer
					journal := filepath.Join(t.TempDir(), "journal.jsonl")
					r := applyRunner{cfg: cfg, stdout: &out, stderr: &stderr, pb: pb, vars: map[string]string{}, window: "w1", yes: true, journalPath: journal, toIdx: 1}
					err := r.execute()
					if err == nil || !strings.Contains(err.Error(), "pcb.fill.create") || !strings.Contains(err.Error(), "kept-fill") || creates != 1 {
						t.Fatalf("fill failure bypassed: err=%v creates=%d stdout=%s stderr=%s", err, creates, out.String(), stderr.String())
					}
					calls := state.snapshot()
					for i, call := range calls {
						if call.Action == "pcb.fill.create" && i != len(calls)-1 {
							t.Fatalf("action after unverified fill: %+v", calls[i+1:])
						}
					}
					if strings.Contains(stderr.String(), "continue anyway?") {
						t.Fatalf("must stop without asking to continue: %s", stderr.String())
					}
					log, readErr := os.ReadFile(journal)
					if readErr != nil || !bytes.Contains(log, []byte(`"status":"fail"`)) || bytes.Contains(log, []byte(`"status":"ok"`)) || !bytes.Contains(log, []byte("kept-fill")) {
						t.Fatalf("journal lost fill failure/identity: %s (%v)", log, readErr)
					}
				})
			}
		}
	}
}
