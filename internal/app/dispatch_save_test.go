package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestSaveCommandsRequireConfirmedSave(t *testing.T) {
	for _, family := range []string{"sch", "pcb"} {
		for _, tc := range []struct {
			name, result string
			wantError    bool
		}{
			{"confirmed", `{"saved":true,"partial":false}`, false},
			{"confirmed without partial", `{"saved":true}`, false},
			{"false preserves partial evidence", `{"saved":false,"partial":true,"reason":"upload failed","writtenId":"keep-id"}`, true},
			{"false", `{"saved":false}`, true},
			{"missing", `{"warning":"save capability unavailable"}`, true},
			{"null", `{"saved":null}`, true},
			{"string true", `{"saved":"true"}`, true},
			{"numeric true", `{"saved":1}`, true},
			{"object", `{"saved":{"value":true}}`, true},
			{"array", `{"saved":[true]}`, true},
			{"partial", `{"saved":true,"partial":true,"warning":"partial save"}`, true},
		} {
			t.Run(family+"/"+tc.name, func(t *testing.T) {
				response := `{"id":"save-result","ok":true,"result":` + tc.result + `}`
				cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return response })
				defer cleanup()
				var stdout, stderr bytes.Buffer
				cmd := newSchCmd(cfg, &stdout, &stderr)
				wantAction := "schematic.save"
				if family == "pcb" {
					cmd = newPcbCmd(cfg, &stdout, &stderr)
					wantAction = "pcb.save"
				}
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				cmd.SetArgs([]string{"save"})
				err := cmd.Execute()
				if (err != nil) != tc.wantError {
					t.Fatalf("error=%v, wantError=%t", err, tc.wantError)
				}
				if tc.wantError && !strings.Contains(err.Error(), "boolean saved:true required") {
					t.Fatalf("save failure did not explain required confirmation: %v", err)
				}
				if strings.TrimSpace(stdout.String()) != response {
					t.Fatalf("original save evidence lost: %s", stdout.String())
				}
				gotCalls := calls.snapshot()
				if len(gotCalls) != 1 || gotCalls[0].Action != wantAction {
					t.Fatalf("unexpected action/retry: %+v", gotCalls)
				}
			})
		}
	}
}

func TestSaveConfirmationGuardDoesNotChangeOtherActions(t *testing.T) {
	for _, action := range []string{"debug.exec_js", "pcb.components.list", "schematic.check"} {
		t.Run(action, func(t *testing.T) {
			response := `{"ok":true,"result":{"saved":false,"partial":true,"value":"keep other action semantics"}}`
			cfg, calls, cleanup := newAutolayoutTestDaemon(t, func(_ int, _ autolayoutTestCall) string { return response })
			defer cleanup()
			var stdout, stderr bytes.Buffer
			if err := dispatch(cfg, action, "", nil, &stdout, &stderr); err != nil {
				t.Fatalf("save-specific guard changed %s: %v", action, err)
			}
			if strings.TrimSpace(stdout.String()) != response || len(calls.snapshot()) != 1 {
				t.Fatalf("other action output/dispatch changed: %s", stdout.String())
			}
		})
	}
}
