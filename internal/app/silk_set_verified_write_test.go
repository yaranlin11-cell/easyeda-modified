package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const verifiedSilkSetResult = `{"verified":true,"partial":false,"count":1,"requestedIds":["s"],"appliedIds":["s"],"results":[{"primitiveId":"s","ok":true,"verified":true,"attempted":true,"requested":{"mirror":false,"reverse":false,"rotation":0,"fontSize":40,"lineWidth":6,"x":10,"y":20,"fontFamily":"Arial","text":"RESET"},"actual":{"primitiveId":"s","mirror":false,"reverse":false,"rotation":0,"fontSize":40,"lineWidth":6,"x":10,"y":20,"fontFamily":"Arial","text":"RESET"}}]}`

func TestSilkSetFreshResultRequiredAcrossEntryPoints(t *testing.T) {
	results := []string{
		verifiedSilkSetResult,
		`{"count":1,"results":[{"primitiveId":"s","ok":true,"verified":true,"fontFamily":"Arial"}]}`,
		strings.Replace(verifiedSilkSetResult, `"partial":false`, `"partial":true`, 1),
		strings.Replace(verifiedSilkSetResult, `"count":1`, `"count":2`, 1),
		strings.Replace(verifiedSilkSetResult, `"appliedIds":["s"]`, `"appliedIds":[]`, 1),
		strings.Replace(verifiedSilkSetResult, `"attempted":true`, `"attempted":false`, 1),
	}
	for _, field := range []string{`"mirror":false`, `"reverse":false`, `"rotation":0`, `"fontSize":40`, `"lineWidth":6`, `"x":10`, `"y":20`, `"fontFamily":"Arial"`, `"text":"RESET"`} {
		// Replace only the last (fresh actual) occurrence, not the request.
		i := strings.LastIndex(verifiedSilkSetResult, field)
		colon := strings.Index(field, ":")
		results = append(results, verifiedSilkSetResult[:i]+field[:colon+1]+"null"+verifiedSilkSetResult[i+len(field):])
	}
	for _, result := range results {
		for _, entry := range []string{"dispatch", "capture", "request", "apply"} {
			t.Run(entry+result, func(t *testing.T) {
				response := `{"ok":true,"result":` + result + `}`
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return response })
				defer cleanup()
				var out, stderr bytes.Buffer
				var err error
				switch entry {
				case "dispatch":
					err = dispatch(cfg, "pcb.silk.set", "w1", nil, &out, &stderr)
				case "capture":
					_, err = dispatchCapture(cfg, "pcb.silk.set", "w1", nil, &out)
				case "request":
					var res *actionResult
					res, err = requestAction(cfg, "pcb.silk.set", "w1", nil)
					if res == nil || res.Result == nil {
						t.Fatal("lost partial result")
					}
				case "apply":
					r := applyRunner{cfg: cfg, window: "w1", stdout: &out}
					var res any
					res, err = r.runAction("pcb.silk.set", nil, time.Second)
					if res == nil {
						t.Fatal("lost partial result")
					}
				}
				wantFailure := result != verifiedSilkSetResult
				if (err != nil) != wantFailure {
					t.Fatalf("err=%v; result=%s", err, result)
				}
				if wantFailure {
					var typed *unverifiedSilkSetError
					if !errors.As(err, &typed) || typed.rawResponse != response {
						t.Fatalf("lost typed error/exact evidence: %v", err)
					}
				}
				if entry == "dispatch" || entry == "capture" || (entry == "apply" && wantFailure) {
					if strings.TrimSpace(out.String()) != response {
						t.Fatalf("lost exact response: %s", out.String())
					}
				}
				if len(calls.snapshot()) != 1 {
					t.Fatalf("blind replay: %+v", calls.snapshot())
				}
			})
		}
	}
}

func TestSilkSetCLIOnlySendsChangedBooleansIncludingFalse(t *testing.T) {
	for _, tc := range []struct {
		name            string
		args            []string
		mirror, reverse any
	}{
		{"omitted", []string{"--rotation", "0"}, nil, nil},
		{"false", []string{"--mirror=false", "--reverse=false"}, false, false},
		{"true", []string{"--mirror", "--reverse"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return `{"ok":true,"result":` + verifiedSilkSetResult + `}` })
			defer cleanup()
			var out, stderr bytes.Buffer
			cmd := newPcbCmd(cfg, &out, &stderr)
			cmd.SetArgs(append([]string{"silk-set", "--ids", "s"}, tc.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			call := calls.snapshot()[0]
			if call.Action != "pcb.silk.set" || call.Payload["mirror"] != tc.mirror || call.Payload["reverse"] != tc.reverse {
				t.Fatalf("payload=%+v", call)
			}
		})
	}
}

func TestSilkSetApplyCannotRetryVerifyOrContinueAndJournalsRawEvidence(t *testing.T) {
	response := `{"ok":true,"result":{"verified":false,"partial":true,"appliedIds":["s"],"results":[{"primitiveId":"s","verified":false,"error":"mirror ignored"}]}}`
	for _, entry := range []string{"action", "pcb silk-set"} {
		for _, mode := range []string{"default", "verify", "retry", "continue", "default-continue", "prompt"} {
			t.Run(entry+"/"+mode, func(t *testing.T) {
				sets := 0
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
					if call.Action == "pcb.silk.set" {
						sets++
						return response
					}
					return `{"ok":true,"result":{}}`
				})
				defer cleanup()
				step := playbookStep{ID: "bad-silk", Action: "pcb.silk.set", Payload: map[string]any{"primitiveIds": []string{"s"}, "mirror": false}}
				if entry != "action" {
					step.Action, step.Run = "", entry
					step.Flags = map[string]any{"ids": "s", "mirror": false}
				}
				pb := &playbook{Version: 1}
				switch mode {
				case "verify":
					step.Verify = &verifyBlock{Action: "pcb.silk.list"}
				case "retry":
					retries := 1
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
				r := applyRunner{cfg: cfg, stdout: &out, stderr: &stderr, pb: pb, vars: map[string]string{}, window: "w1", yes: true, journalPath: journal, toIdx: 1, quiet: true}
				err := r.execute()
				var typed *unverifiedSilkSetError
				if !errors.As(err, &typed) || sets != 1 {
					t.Fatalf("failure bypassed: %v; sets=%d", err, sets)
				}
				list := calls.snapshot()
				if list[len(list)-1].Action != "pcb.silk.set" || strings.Contains(stderr.String(), "continue anyway?") {
					t.Fatalf("dependent action/prompt: %+v %s", list, stderr.String())
				}
				log, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, line := range bytes.Split(log, []byte("\n")) {
					var item journalEntry
					if json.Unmarshal(line, &item) == nil && item.Status == "fail" {
						found = item.RawResponse == response
					}
				}
				if !found || bytes.Contains(log, []byte(`"status":"ok"`)) {
					t.Fatalf("lost exact failed response: %s", log)
				}
			})
		}
	}
}
