package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Both collectors are readable and empty; the template without writable title
// keys supplies a real Go-side INFO finding, matching the Web fixture's case.
func newSchCheckVerdictFixture(t *testing.T, level string, connectorPassed, titleInfo bool) *appConfig {
	t.Helper()
	cfg, _, cleanup := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
		var result any
		switch call.Action {
		case "schematic.check":
			findings := []any{}
			if level != "" {
				findings = append(findings, map[string]any{"type": "wire-crossing", "level": level, "message": "preserve native finding"})
			}
			result = map[string]any{"passed": connectorPassed, "summary": map[string]any{"total": len(findings)}, "findings": findings}
		case "schematic.components.list":
			result = map[string]any{"components": []any{}, "count": 0}
		case "document.current":
			result = map[string]any{"uuid": "check-page", "documentType": "schematic", "parentProjectUuid": "check-project"}
		case "debug.exec_js":
			code, _ := call.Payload["code"].(string)
			if strings.Contains(code, "sch_PrimitiveWire.getAll") {
				result = map[string]any{"value": map[string]any{"wires": []any{}}}
			} else {
				result = map[string]any{"value": map[string]any{"rectangles": []any{}, "texts": []any{}, "designators": []any{}}}
			}
		case "schematic.titleblock.get":
			data := map[string]any{}
			if !titleInfo {
				for _, key := range []string{"Name", "Drawed", "Description"} {
					data[key] = map[string]any{"value": "provided"}
				}
			}
			result = map[string]any{"titleBlockData": data}
		default:
			t.Errorf("unexpected action: %s", call.Action)
			result = map[string]any{}
		}
		body, err := json.Marshal(map[string]any{"ok": true, "result": result, "context": map[string]any{"documentUuid": "check-page", "documentType": "schematic", "projectUuid": "check-project"}})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	})
	t.Cleanup(cleanup)
	return cfg
}

func TestSchCheckCommandFinalVerdictUsesSeverity(t *testing.T) {
	for _, tc := range []struct {
		name, level                string
		connectorPassed, titleInfo bool
		wantPassed                 bool
		wantCount                  int
	}{
		{"clean", "", true, false, true, 0},
		{"connector INFO without new geometry", "info", false, false, true, 1},
		{"Go title INFO", "", true, true, true, 1},
		{"mixed INFO", "INFO", true, true, true, 2},
		{"WARN with INFO", "warn", true, true, false, 2},
		{"ERROR with INFO", "ERROR", true, true, false, 2},
		{"FATAL with INFO", "fatal", true, true, false, 2},
		{"unknown with INFO", "unknown", true, true, false, 2},
	} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%t", tc.name, strict), func(t *testing.T) {
				cfg := newSchCheckVerdictFixture(t, tc.level, tc.connectorPassed, tc.titleInfo)
				var stdout, stderr bytes.Buffer
				cmd := newSchCmd(cfg, &stdout, &stderr)
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				args := []string{"check", "--json"}
				if strict {
					args = append(args, "--strict")
				}
				cmd.SetArgs(args)
				err := cmd.Execute()
				if (err != nil) != (strict && !tc.wantPassed) {
					t.Fatalf("error=%v; stderr=%s stdout=%s", err, stderr.String(), stdout.String())
				}
				var out struct {
					OK     bool        `json:"ok"`
					Result checkReport `json:"result"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
					t.Fatalf("invalid report: %v\n%s", err, stdout.String())
				}
				if !out.OK || out.Result.Passed != tc.wantPassed || out.Result.Summary.Total != tc.wantCount || len(out.Result.Findings) != tc.wantCount {
					t.Fatalf("inconsistent final verdict/findings: %+v", out)
				}
			})
		}
	}
}

func TestSchCheckCommandInfoHumanOutputDoesNotClaimNoFindings(t *testing.T) {
	cfg := newSchCheckVerdictFixture(t, "info", true, true)
	var stdout, stderr bytes.Buffer
	cmd := newSchCmd(cfg, &stdout, &stderr)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"check", "--strict"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("INFO-only check should pass: %v\n%s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "✓ no findings") ||
		!strings.Contains(stdout.String(), "✓ no blocking findings (2 advisory finding(s))") ||
		!strings.Contains(stdout.String(), "titleblock-key-absent") ||
		!strings.Contains(stdout.String(), "wire-crossing") {
		t.Fatalf("advisory report was misleading or lost its details: %s", stdout.String())
	}
}
