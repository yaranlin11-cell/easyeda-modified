package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPcbDrcCommandRequiresNativePassed(t *testing.T) {
	for _, tc := range []struct {
		name, result string
		wantError    bool
	}{
		{"passed", `{"passed":true,"violations":[],"partial":false}`, false},
		{"failed", `{"passed":false,"violations":[{"name":"new-native-group","list":[{"newDetail":"keep me"}]}],"partial":true}`, true},
		{"missing verdict", `{"violations":[],"diagnostic":"native verdict missing"}`, true},
		{"null verdict", `{"passed":null,"violations":[]}`, true},
		{"string verdict", `{"passed":"true","violations":[]}`, true},
		{"numeric verdict", `{"passed":1,"violations":[]}`, true},
		{"null result", `null`, true},
	} {
		for _, flat := range []bool{false, true} {
			for _, strict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/json=%t/strict=%t", tc.name, flat, strict), func(t *testing.T) {
					response := `{"id":"drc-result","type":"response","version":"v1","ok":true,"result":` + tc.result + `}`
					cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
						if call.Action != "pcb.drc.check" {
							t.Errorf("unexpected action: %s", call.Action)
						}
						return response
					})
					defer cleanup()
					var stdout, stderr bytes.Buffer
					cmd := newPcbCmd(cfg, &stdout, &stderr)
					cmd.SetOut(&stdout)
					cmd.SetErr(&stderr)
					cmd.SilenceErrors, cmd.SilenceUsage = true, true
					args := []string{"drc", "--timeout", "120"}
					if flat {
						args = append(args, "--json")
					}
					if strict {
						args = append(args, "--strict")
					}
					cmd.SetArgs(args)
					err := cmd.Execute()
					if (err != nil) != tc.wantError {
						t.Fatalf("error=%v, wantError=%t; stdout=%s", err, tc.wantError, stdout.String())
					}
					if tc.wantError && !strings.Contains(err.Error(), "native verdict") {
						t.Fatalf("lost native verdict diagnostic: %v", err)
					}
					if !flat {
						if strings.TrimSpace(stdout.String()) != response {
							t.Fatalf("raw result changed or was discarded: %s", stdout.String())
						}
					} else {
						var out struct {
							drcFlatReport
							NativeResult map[string]any `json:"nativeResult"`
						}
						if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
							t.Fatalf("invalid flat output: %v\n%s", err, stdout.String())
						}
						var want map[string]any
						if err := json.Unmarshal([]byte(tc.result), &want); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(out.NativeResult, want) {
							t.Fatalf("native result lost: got=%#v want=%#v", out.NativeResult, want)
						}
						if out.Passed != !tc.wantError {
							t.Fatalf("flat passed=%t, want=%t", out.Passed, !tc.wantError)
						}
					}
					gotCalls := calls.snapshot()
					if len(gotCalls) != 1 || gotCalls[0].TimeoutMs != 120000 || (gotCalls[0].Payload["strict"] == true) != strict {
						t.Fatalf("unexpected retry/timeout/strict dispatch: %+v", gotCalls)
					}
				})
			}
		}
	}
}

func TestPcbDrcCommandStillEmitsRecognizedViolationsOnFailure(t *testing.T) {
	result, err := json.Marshal(loadDrcFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string {
		return `{"ok":true,"result":` + string(result) + `}`
	})
	defer cleanup()
	var stdout, stderr bytes.Buffer
	cmd := newPcbCmd(cfg, &stdout, &stderr)
	cmd.SetArgs([]string{"drc", "--json"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("failed native DRC exited successfully")
	}
	var out drcFlatReport
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Passed || out.Total != 4 || out.Counts["Connection Error"] != 2 || len(out.Violations) != 4 || len(calls.snapshot()) != 1 {
		t.Fatalf("failure output or dispatch changed: %+v", out)
	}
}

func TestPcbDrcCommandPreservesFailedActionAndMalformedResult(t *testing.T) {
	for _, response := range []string{
		`{"ok":false,"result":{"passed":false,"partial":true,"diagnostic":"native partial evidence"},"error":{"code":"DRC_FAILED","message":"host failed"}}`,
		`{"ok":true,"result":[{"nativeUnknown":"keep this result"}]}`,
		`{"ok":true,"result":"native result is unreadable"}`,
		`{"ok":true,"result":{"passed":true}`, // malformed JSON remains evidence
	} {
		for _, flat := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", response, flat), func(t *testing.T) {
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return response })
				defer cleanup()
				var stdout, stderr bytes.Buffer
				cmd := newPcbCmd(cfg, &stdout, &stderr)
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				args := []string{"drc"}
				if flat {
					args = append(args, "--json")
				}
				cmd.SetArgs(args)
				if err := cmd.Execute(); err == nil {
					t.Fatal("failed action/malformed native result exited successfully")
				}
				if strings.TrimSpace(stdout.String()) != response || len(calls.snapshot()) != 1 {
					t.Fatalf("native evidence discarded or action retried: %s", stdout.String())
				}
			})
		}
	}
}
