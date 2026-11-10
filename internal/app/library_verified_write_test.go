package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type libraryFailureOutcome struct {
	name, raw, code string
	abort           bool
}

var libraryUnknownOutcomes = []libraryFailureOutcome{
	{name: "dispatch-timeout", raw: `{"ok":false,"error":{"code":"DISPATCH_FAILED","message":"connector did not respond","detail":"context deadline exceeded"}}`, code: "DISPATCH_FAILED"},
	{name: "precondition", raw: `{"ok":false,"error":{"code":"PRECONDITION_REFUSED","message":"target is not empty"}}`, code: "PRECONDITION_REFUSED"},
	{name: "queue-blocked", raw: `{"ok":false,"error":{"code":"CONNECTOR_QUEUE_BLOCKED","message":"action was not sent"}}`, code: connectorQueueBlockedCode},
	{name: "malformed", raw: `{"ok":true,"result":`},
	{name: "transport", abort: true},
}

func newLibraryFailureTestDaemon(t *testing.T, outcome libraryFailureOutcome) (*appConfig, *autolayoutTestDaemon, func()) {
	return newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string {
		if outcome.abort {
			// The request reached the daemon, but no response reached the client.
			panic(http.ErrAbortHandler)
		}
		return outcome.raw
	})
}

func checkLibraryUnknownFailure(t *testing.T, err error, outcome libraryFailureOutcome) {
	t.Helper()
	var libraryErr *unverifiedLibraryWriteError
	if !errors.As(err, &libraryErr) || libraryErr.rawResponse != outcome.raw || libraryErr.cause == nil {
		t.Fatalf("lost write stop, raw response or cause: %v", err)
	}
	if outcome.code != "" {
		var actionErr *actionError
		if !errors.As(err, &actionErr) || actionErr.Code != outcome.code {
			t.Fatalf("lost original action error: %v", err)
		}
	}
}

func TestLibraryWriteVerificationAcrossEntrypoints(t *testing.T) {
	for _, action := range []string{"library.footprint.build", "schematic.rebind.footprint"} {
		for _, result := range []string{`{}`, `{"verified":false}`, `{"verified":true,"partial":true}`, `{"verified":true}`} {
			for _, entry := range []string{"dispatch", "capture", "request", "apply"} {
				t.Run(action+result+entry, func(t *testing.T) {
					raw := `{"ok":true,"result":` + result + `}`
					cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return raw })
					defer cleanup()
					var out, stderr bytes.Buffer
					var err error
					switch entry {
					case "dispatch":
						err = dispatch(cfg, action, "w1", map[string]any{}, &out, &stderr)
						if bytes.TrimSpace(out.Bytes())[0] != '{' {
							t.Fatal("response lost")
						}
					case "capture":
						var res *actionResult
						res, err = dispatchCapture(cfg, action, "w1", map[string]any{}, &out)
						if res == nil || out.String() != raw+"\n" {
							t.Fatal("response lost")
						}
					case "request":
						var res *actionResult
						res, err = requestAction(cfg, action, "w1", map[string]any{})
						if res == nil {
							t.Fatal("response lost")
						}
					case "apply":
						r := applyRunner{cfg: cfg, window: "w1"}
						var res any
						res, err = r.runAction(action, map[string]any{}, time.Second)
						if res == nil {
							t.Fatal("response lost")
						}
					}
					if (err == nil) != (result == `{"verified":true}`) {
						t.Fatalf("result=%s err=%v", result, err)
					}
					if err != nil {
						var e *unverifiedLibraryWriteError
						if !errors.As(err, &e) || e.rawResponse != raw {
							t.Fatalf("lost typed error/raw: %v", err)
						}
					}
					if len(calls.snapshot()) != 1 {
						t.Fatal("unexpected retry")
					}
				})
			}
		}
	}
}

func TestLibraryWriteApplyCannotRetryVerifyOrContinue(t *testing.T) {
	for _, action := range []string{"library.footprint.build", "schematic.rebind.footprint"} {
		for _, mode := range []string{"retry", "verify", "continue", "default-continue"} {
			t.Run(action+mode, func(t *testing.T) {
				raw := `{"ok":true,"result":{"verified":false,"partial":true,"created":{"pads":["retained-pad"]}}}`
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return raw })
				defer cleanup()
				step := playbookStep{ID: "write", Action: action, Payload: map[string]any{}}
				pb := &playbook{Version: 1}
				switch mode {
				case "retry":
					n := 2
					step.Retry = &n
				case "verify":
					step.Verify = &verifyBlock{Action: "pcb.list"}
				case "continue":
					step.OnFail = "continue"
				case "default-continue":
					v := true
					pb.Defaults.ContinueOnError = &v
				}
				pb.Steps = []playbookStep{step, {ID: "dependent", Action: "pcb.line.create", Payload: map[string]any{}}}
				var out, stderr bytes.Buffer
				journal := filepath.Join(t.TempDir(), "journal.jsonl")
				r := applyRunner{cfg: cfg, window: "w1", pb: pb, stdout: &out, stderr: &stderr, vars: map[string]string{}, yes: true, journalPath: journal, toIdx: 1}
				err := r.execute()
				var e *unverifiedLibraryWriteError
				if !errors.As(err, &e) || len(calls.snapshot()) != 1 {
					t.Fatalf("bypassed write failure err=%v calls=%v", err, calls.snapshot())
				}
				log, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				var row map[string]any
				rows := bytes.Split(bytes.TrimSpace(log), []byte("\n"))
				if err = json.Unmarshal(rows[len(rows)-1], &row); err != nil {
					t.Fatal(err)
				}
				if row["status"] != "fail" || row["rawResponse"] != raw {
					t.Fatalf("lost evidence: %s", log)
				}
			})
		}
	}
}

func TestLibraryWriteUnknownOutcomeAcrossEntrypoints(t *testing.T) {
	for _, action := range []string{"library.footprint.build", "schematic.rebind.footprint"} {
		for _, outcome := range libraryUnknownOutcomes {
			for _, entry := range []string{"dispatch", "capture", "request", "apply"} {
				t.Run(action+"/"+outcome.name+"/"+entry, func(t *testing.T) {
					cfg, calls, cleanup := newLibraryFailureTestDaemon(t, outcome)
					defer cleanup()
					var out, stderr bytes.Buffer
					var err error
					switch entry {
					case "dispatch":
						err = dispatchTimed(cfg, action, "w1", map[string]any{}, time.Second, &out, &stderr)
						if !outcome.abort && out.String() != outcome.raw+"\n" {
							t.Fatalf("response lost from stdout: %q", out.String())
						}
					case "capture":
						_, err = dispatchCapture(cfg, action, "w1", map[string]any{}, &out)
						if !outcome.abort && out.String() != outcome.raw+"\n" {
							t.Fatalf("response lost from stdout: %q", out.String())
						}
					case "request":
						_, err = requestActionTimed(cfg, action, "w1", map[string]any{}, time.Second)
					case "apply":
						r := applyRunner{cfg: cfg, window: "w1"}
						_, err = r.runAction(action, map[string]any{}, time.Second)
					}
					checkLibraryUnknownFailure(t, err, outcome)
					if len(calls.snapshot()) != 1 {
						t.Fatalf("unexpected library retry: %v", calls.snapshot())
					}
				})
			}
		}
	}
}

func TestLibraryWriteUnknownApplyCannotRecover(t *testing.T) {
	for _, action := range []string{"library.footprint.build", "schematic.rebind.footprint"} {
		for _, outcome := range libraryUnknownOutcomes {
			for _, mode := range []string{"retry", "verify", "continue", "default-continue", "run"} {
				if mode == "run" && action != "library.footprint.build" {
					continue
				}
				t.Run(action+"/"+outcome.name+"/"+mode, func(t *testing.T) {
					cfg, calls, cleanup := newLibraryFailureTestDaemon(t, outcome)
					defer cleanup()
					step := playbookStep{ID: "write", Action: action, Payload: map[string]any{}}
					pb := &playbook{Version: 1}
					switch mode {
					case "retry":
						n := 2
						step.Retry = &n
					case "verify":
						step.Verify = &verifyBlock{Action: "pcb.list"}
					case "continue":
						step.OnFail = "continue"
					case "default-continue":
						v := true
						pb.Defaults.ContinueOnError = &v
					case "run":
						specPath := filepath.Join(t.TempDir(), "footprint.json")
						if err := os.WriteFile(specPath, []byte(`{"pads":[]}`), 0o600); err != nil {
							t.Fatal(err)
						}
						step.Action, step.Payload = "", nil
						step.Run = "lib footprint build --uuid fp --library lib --spec " + specPath
						n := 2
						step.Retry, step.Verify, step.OnFail = &n, &verifyBlock{Action: "pcb.list"}, "continue"
					}
					pb.Steps = []playbookStep{step, {ID: "dependent", Action: "pcb.line.create", Payload: map[string]any{}}}
					var out, stderr bytes.Buffer
					journal := filepath.Join(t.TempDir(), "journal.jsonl")
					r := applyRunner{cfg: cfg, window: "w1", pb: pb, stdout: &out, stderr: &stderr, vars: map[string]string{}, yes: true, journalPath: journal, toIdx: 1}
					err := r.execute()
					checkLibraryUnknownFailure(t, err, outcome)
					if len(calls.snapshot()) != 1 {
						t.Fatalf("unknown write recovered/replayed: %v", calls.snapshot())
					}
					log, err := os.ReadFile(journal)
					if err != nil {
						t.Fatal(err)
					}
					var row map[string]any
					rows := bytes.Split(bytes.TrimSpace(log), []byte("\n"))
					if err := json.Unmarshal(rows[len(rows)-1], &row); err != nil {
						t.Fatal(err)
					}
					if row["status"] != "fail" || (!outcome.abort && row["rawResponse"] != outcome.raw) {
						t.Fatalf("lost failure evidence: %s", log)
					}
				})
			}
		}
	}
}
