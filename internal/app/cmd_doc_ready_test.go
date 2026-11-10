package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const readyTestUUID = "1111111111111111"

type readyTestChange func(action string, listSample, currentSample int, response map[string]any)

func newReadyTestFixture(t *testing.T, change readyTestChange) (*appConfig, *blockApplyTestDaemon, func()) {
	return newReadyTestFixtureForType(t, "schematic", change)
}

func newReadyTestFixtureForType(t *testing.T, docType string, change readyTestChange) (*appConfig, *blockApplyTestDaemon, func()) {
	t.Helper()
	lists, currents, requests := 0, 0, 0
	cfg, server, cleanup := newBlockApplyTestDaemon(t, func(call blockApplyTestCall) string {
		requests++
		context := map[string]any{"projectUuid": "ready-project", "projectName": "Ready fixture", "documentUuid": readyTestUUID, "documentType": docType, "tabId": "ready-tab"}
		result := map[string]any{}
		switch call.Action {
		case "document.current":
			currents++
			result = map[string]any{"uuid": readyTestUUID, "documentType": docType, "tabId": "ready-tab", "parentProjectUuid": "ready-project"}
		case "schematic.components.list", "pcb.components.list":
			lists++
			result = map[string]any{"count": float64(2), "components": []any{
				map[string]any{"primitiveId": "part-a", "x": float64(10), "otherProperty": map[string]any{"Value": "1k"}},
				map[string]any{"primitiveId": "part-b", "x": float64(20)},
			}}
		default:
			t.Errorf("readiness invoked forbidden action %s", call.Action)
		}
		response := map[string]any{"ok": true, "id": fmt.Sprintf("ready-%d", requests), "context": context, "result": result, "seq": float64(requests), "seqAbandoned": float64(0)}
		if change != nil {
			change(call.Action, lists, currents, response)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Error(err)
			return `{"ok":false}`
		}
		return string(encoded)
	})
	cfg.project = "ready-project"
	return cfg, server, cleanup
}

func readyTestListCount(server *blockApplyTestDaemon) int {
	count := 0
	for _, call := range server.snapshot() {
		if call.Action == "schematic.components.list" || call.Action == "pcb.components.list" {
			count++
		}
	}
	return count
}

func TestDocumentReadyRequiresConsecutiveFullInventories(t *testing.T) {
	for _, scenario := range []struct {
		name                        string
		samples, stable, components int
	}{{"normal", 2, 2, 2}, {"empty", 3, 3, 0}, {"reordered", 2, 2, 2}, {"same-count-content-change", 3, 2, 2}, {"same-count-id-change", 3, 2, 2}} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg, server, cleanup := newReadyTestFixture(t, func(action string, sample, _ int, response map[string]any) {
				if action != "schematic.components.list" {
					return
				}
				result := response["result"].(map[string]any)
				rows := result["components"].([]any)
				switch scenario.name {
				case "empty":
					result["components"], result["count"] = []any{}, float64(0)
				case "reordered":
					if sample%2 == 0 {
						rows[0], rows[1] = rows[1], rows[0]
					}
				case "same-count-content-change":
					if sample >= 2 {
						rows[0].(map[string]any)["otherProperty"] = map[string]any{"Value": "2k"}
					}
				case "same-count-id-change":
					if sample >= 2 {
						rows[0].(map[string]any)["primitiveId"] = "part-c"
					}
				}
			})
			defer cleanup()
			observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if !observation.Ready || observation.Scope != "component-inventory-readiness" || observation.Samples != scenario.samples || observation.StableSamples != scenario.stable || observation.ComponentCount != scenario.components {
				t.Fatalf("unexpected observation: %+v", observation)
			}
			if readyTestListCount(server) != scenario.samples || len(observation.RequestIDs) != 1+2*scenario.samples || len(observation.ComponentSHA256) != 64 {
				t.Fatalf("incomplete sample/hash/request evidence: %+v", observation)
			}
			if _, err := time.Parse(time.RFC3339Nano, observation.ObservedAt); err != nil {
				t.Fatalf("invalid observation timestamp: %v", err)
			}
		})
	}
}

func TestDocumentReadyPCBInventoryAllowsUnrequestedGeometry(t *testing.T) {
	cfg, server, cleanup := newReadyTestFixtureForType(t, "pcb", func(action string, _, _ int, response map[string]any) {
		if action != "pcb.components.list" {
			return
		}
		result := response["result"].(map[string]any)
		result["partial"], result["incomplete"] = false, false
		for _, row := range result["components"].([]any) {
			component := row.(map[string]any)
			component["availability"] = map[string]any{"pads": "not-requested", "bbox": "not-requested"}
			component["captureErrors"] = map[string]any{}
		}
	})
	defer cleanup()
	observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, 2*time.Second)
	if err != nil || observation == nil || !observation.Ready || observation.Active.DocumentType != "pcb" {
		t.Fatalf("PCB inventory failed: %+v %v", observation, err)
	}
	if readyTestListCount(server) != 2 {
		t.Fatal("PCB readiness skipped stable sampling")
	}
	for _, call := range server.snapshot() {
		if call.Action != "document.current" && call.Action != "pcb.components.list" {
			t.Fatalf("PCB readiness used wrong action: %s", call.Action)
		}
	}
}

func TestDocumentReadyAcceptsBypassCurrentWithOrderedInventory(t *testing.T) {
	for _, docType := range []string{"pcb", "schematic"} {
		t.Run(docType, func(t *testing.T) {
			cfg, server, cleanup := newReadyTestFixtureForType(t, docType, func(action string, _, _ int, response map[string]any) {
				response["unordered"] = action == "document.current"
			})
			defer cleanup()
			observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, 2*time.Second)
			if err != nil || observation == nil || !observation.Ready || observation.StableSamples != 2 || readyTestListCount(server) != 2 {
				t.Fatalf("bypass current must not invalidate ordered inventory: observation=%+v err=%v", observation, err)
			}
		})
	}
}

func TestDocumentReadyRejectsPartialCaptureEvidence(t *testing.T) {
	for _, scope := range []string{"result", "component"} {
		for _, scenario := range []struct {
			name, field string
			value       any
		}{
			{"partial", "partial", true}, {"partial-null", "partial", nil}, {"partial-string", "partial", "false"},
			{"incomplete", "incomplete", true}, {"incomplete-number", "incomplete", float64(0)},
			{"capture-error", "captureErrors", map[string]any{"pads": map[string]any{"code": "getter-threw"}}},
			{"capture-errors-null", "captureErrors", nil}, {"capture-errors-array", "captureErrors", []any{}},
			{"unavailable", "availability", map[string]any{"pads": "unavailable"}},
			{"availability-null", "availability", nil}, {"availability-array", "availability", []any{}},
			{"availability-unknown", "availability", map[string]any{"pads": "unknown"}},
			{"availability-malformed", "availability", map[string]any{"pads": map[string]any{}}},
		} {
			t.Run(scope+"/"+scenario.name, func(t *testing.T) {
				component := map[string]any{"primitiveId": "part-a"}
				result := map[string]any{"components": []any{component}, "count": float64(1)}
				target := result
				if scope == "component" {
					target = component
				}
				target[scenario.field] = scenario.value
				if _, _, err := readyComponentInventory(result); err == nil {
					t.Fatal("incomplete capture evidence accepted")
				}
			})
		}
	}
}

func TestDocumentReadyRejectsIdentityAndInventoryFailures(t *testing.T) {
	for _, scenario := range []string{"missing-current-context", "current-result-disagrees", "parent-project-disagrees", "missing-list-context", "list-context-drift", "after-current-drift", "unordered-list", "missing-array", "wrong-array-type", "invalid-row", "duplicate-id", "missing-id", "count-disagrees"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, _, cleanup := newReadyTestFixture(t, func(action string, _, currentSample int, response map[string]any) {
				result := response["result"].(map[string]any)
				context := response["context"].(map[string]any)
				if action == "document.current" {
					switch scenario {
					case "missing-current-context":
						delete(response, "context")
					case "current-result-disagrees":
						result["uuid"] = "other-document"
					case "parent-project-disagrees":
						result["parentProjectUuid"] = "other-project"
					case "after-current-drift":
						if currentSample > 1 {
							result["tabId"], context["tabId"] = "other-tab", "other-tab"
						}
					}
					return
				}
				if action != "schematic.components.list" {
					return
				}
				switch scenario {
				case "missing-list-context":
					delete(response, "context")
				case "list-context-drift":
					context["documentUuid"] = "other-document"
				case "unordered-list":
					response["unordered"] = true
				case "missing-array":
					delete(result, "components")
				case "wrong-array-type":
					result["components"] = map[string]any{}
				case "invalid-row":
					result["components"].([]any)[0] = "bad-row"
				case "duplicate-id":
					result["components"].([]any)[1].(map[string]any)["primitiveId"] = "part-a"
				case "missing-id":
					delete(result["components"].([]any)[0].(map[string]any), "primitiveId")
				case "count-disagrees":
					result["count"] = float64(3)
				}
			})
			defer cleanup()
			if observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, time.Second); err == nil || observation != nil {
				t.Fatalf("invalid readiness evidence accepted: %+v, %v", observation, err)
			}
		})
	}
}

func TestDocumentReadyNeverNavigatesForTargetOrConfiguredDoc(t *testing.T) {
	for _, scenario := range []string{"wrong-target", "stale-config-with-explicit-target", "stale-config-without-target"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, server, cleanup := newReadyTestFixture(t, nil)
			defer cleanup()
			target := readyTestUUID
			cfg.doc = "2222222222222222"
			if scenario == "wrong-target" {
				target = "3333333333333333"
			}
			if scenario == "stale-config-without-target" {
				target = ""
			}
			observation, err := verifyDocumentReady(cfg, "w1", target, 2*time.Second)
			if scenario == "stale-config-with-explicit-target" {
				if err != nil || observation == nil || !observation.Ready {
					t.Fatalf("explicit matching target failed: %+v %v", observation, err)
				}
			} else if err == nil || observation != nil {
				t.Fatalf("wrong target was accepted: %+v %v", observation, err)
			}
			if cfg.doc != "2222222222222222" {
				t.Fatal("readiness mutated caller document selector")
			}
			for _, call := range server.snapshot() {
				if call.Action != "document.current" && call.Action != "schematic.components.list" {
					t.Fatalf("readiness navigated or wrote: %s", call.Action)
				}
			}
			if scenario != "stale-config-with-explicit-target" && readyTestListCount(server) != 0 {
				t.Fatal("wrong target was sampled before rejection")
			}
		})
	}
}

func TestDocumentReadyQueueBlockedHasNoRetry(t *testing.T) {
	cfg, server, cleanup := newReadyTestFixture(t, func(action string, _, _ int, response map[string]any) {
		if action == "schematic.components.list" {
			response["ok"] = false
			response["error"] = map[string]any{"code": "QUEUE_BLOCKED", "message": "synthetic queue still blocked"}
		}
	})
	defer cleanup()
	started := time.Now()
	if observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, time.Second); err == nil || observation != nil {
		t.Fatalf("queue-blocked read accepted: %+v %v", observation, err)
	}
	if readyTestListCount(server) != 1 || len(server.snapshot()) != 2 {
		t.Fatalf("queue-blocked action was retried: %+v", server.snapshot())
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("queue-blocked readiness entered a retry wait")
	}
}

func TestDocumentReadyTotalDeadlineIncludesPolling(t *testing.T) {
	cfg, server, cleanup := newReadyTestFixture(t, nil)
	defer cleanup()
	started := time.Now()
	if observation, err := verifyDocumentReady(cfg, "w1", readyTestUUID, 70*time.Millisecond); err == nil || observation != nil {
		t.Fatalf("short deadline accepted unstable inventory: %+v %v", observation, err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("readiness exceeded short total budget: %s", elapsed)
	}
	if readyTestListCount(server) > 1 {
		t.Fatal("polling continued beyond the total deadline")
	}
}

func TestDocReadyCommandReportsOnlyInventoryReadiness(t *testing.T) {
	cfg, server, cleanup := newReadyTestFixture(t, nil)
	defer cleanup()
	window := "w1"
	var stdout bytes.Buffer
	cmd := newDocReadyCmd(cfg, &window, &stdout)
	cmd.SetArgs([]string{readyTestUUID, "--json", "--timeout", "2s"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if response["ready"] != true || response["scope"] != "component-inventory-readiness" {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
	for _, forbidden := range []string{"reloaded", "handlerCompleted", "handler-completed"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("readiness falsely claimed %s: %s", forbidden, stdout.String())
		}
	}
	if readyTestListCount(server) != 2 {
		t.Fatal("command skipped independent stable inventory samples")
	}
}
