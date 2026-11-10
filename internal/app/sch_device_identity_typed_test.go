package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Synthetic identities only: this fixture carries no project or customer data.
func typedIdentityFixture(custom bool) map[string]any {
	library := "system"
	if custom {
		library = "personal-library"
	}
	resolution := map[string]any{
		"via": "native-device-source", "sameFootprintUUID": true,
		"footprintSource": map[string]any{
			"instanceUuid": strings.Repeat("c", 16), "uuid": strings.Repeat("d", 32),
			"libraryUuid": "footprint-library", "sourceKind": "project-epro2",
		},
	}
	c := map[string]any{
		"componentType": "part", "primitiveId": "synthetic-part", "designator": "U1",
		"name": "={Manufacturer Part}", "manufacturerId": "SYNTHETIC-PART-A", "supplierId": "",
		"device":           map[string]any{"uuid": strings.Repeat("a", 32), "libraryUuid": library, "name": "={Manufacturer Part}"},
		"placedDevice":     map[string]any{"uuid": strings.Repeat("b", 16), "libraryUuid": library},
		"component":        map[string]any{"uuid": strings.Repeat("b", 16), "libraryUuid": library, "name": "Synthetic part A"},
		"footprint":        map[string]any{"uuid": strings.Repeat("c", 16), "libraryUuid": "footprint-library", "name": "Synthetic footprint"},
		"deviceResolution": resolution,
		"x":                float64(100), "y": float64(200), "rotation": float64(0), "addIntoBom": true,
		"pins": []any{map[string]any{"pinNumber": "1", "x": float64(90), "y": float64(200), "net": "GND", "noConnected": false}},
	}
	if !custom {
		c["supplierId"] = "C123456"
		resolution["lcsc"] = "C123456"
	}
	return c
}

func typedIdentityMapAt(c map[string]any, path ...string) map[string]any {
	for _, key := range path {
		c = c[key].(map[string]any)
	}
	return c
}

func typedIdentityBytes(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte("\n"), b...), '\n')
}

func typedIdentityEnvelope(parts ...map[string]any) map[string]any {
	rows := make([]any, len(parts))
	for i, c := range parts {
		rows[i] = c
	}
	return map[string]any{
		"ok": true, "id": "synthetic-identity-read", "sequence": float64(42),
		"context": map[string]any{"projectUuid": "project", "documentUuid": "page", "documentType": "schematic", "tabId": "tab"},
		"result":  map[string]any{"components": rows, "wires": []any{}, "connectivitySummary": map[string]any{"netCount": float64(1)}},
	}
}

func typedIdentityDecode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("invalid identity read response: %v", err)
	}
	return value
}

func TestSchematicTypedIdentityAcceptsExactNativeProofs(t *testing.T) {
	for _, scenario := range []string{"commercial", "custom-model", "custom-missing-supplier", "custom-component-name", "custom-record-name", "direct-library"} {
		t.Run(scenario, func(t *testing.T) {
			c := typedIdentityFixture(scenario != "commercial")
			if scenario == "custom-missing-supplier" {
				delete(c, "supplierId")
			}
			if strings.Contains(scenario, "name") {
				delete(c, "manufacturerId")
			}
			if scenario == "custom-record-name" {
				typedIdentityMapAt(c, "component")["name"] = "=dynamic"
				c["name"] = "Synthetic custom part"
			}
			if scenario == "direct-library" {
				delete(c, "placedDevice")
				delete(c, "deviceResolution")
			}
			before := typedIdentityBytes(t, c)
			if err := validateSchematicTypedDeviceIdentity(c); err != nil {
				t.Fatalf("complete native identity rejected: %v", err)
			}
			if !bytes.Equal(before, typedIdentityBytes(t, c)) {
				t.Fatal("pure identity validation mutated its input")
			}
		})
	}
}

func TestSchematicTypedIdentityRejectsMissingAndMistypedEvidence(t *testing.T) {
	paths := [][]string{
		{"device"}, {"placedDevice"}, {"component"}, {"footprint"}, {"deviceResolution"}, {"deviceResolution", "footprintSource"},
		{"device", "uuid"}, {"device", "libraryUuid"}, {"placedDevice", "uuid"}, {"placedDevice", "libraryUuid"},
		{"component", "uuid"}, {"footprint", "uuid"}, {"footprint", "libraryUuid"},
		{"deviceResolution", "via"}, {"deviceResolution", "sameFootprintUUID"}, {"deviceResolution", "lcsc"},
		{"deviceResolution", "footprintSource", "instanceUuid"}, {"deviceResolution", "footprintSource", "uuid"},
		{"deviceResolution", "footprintSource", "libraryUuid"}, {"deviceResolution", "footprintSource", "sourceKind"},
	}
	for _, path := range paths {
		for _, mode := range []string{"missing", "null", "wrong-type"} {
			t.Run(strings.Join(path, "/")+"/"+mode, func(t *testing.T) {
				c := typedIdentityFixture(false)
				owner := typedIdentityMapAt(c, path[:len(path)-1]...)
				key := path[len(path)-1]
				switch mode {
				case "missing":
					delete(owner, key)
				case "null":
					owner[key] = nil
				case "wrong-type":
					owner[key] = []any{"not the required field type"}
				}
				before := typedIdentityBytes(t, c)
				if err := validateSchematicTypedDeviceIdentity(c); err == nil {
					t.Fatal("missing or mistyped native identity evidence accepted")
				}
				if !bytes.Equal(before, typedIdentityBytes(t, c)) {
					t.Fatal("failed identity validation mutated its input")
				}
			})
		}
	}
}

func TestSchematicTypedIdentityRejectsConflictsAndExistingErrors(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"device-instance-domain", func(c map[string]any) { typedIdentityMapAt(c, "device")["uuid"] = strings.Repeat("a", 16) }},
		{"device-nonhex", func(c map[string]any) { typedIdentityMapAt(c, "device")["uuid"] = strings.Repeat("z", 32) }},
		{"placed-library-domain", func(c map[string]any) { typedIdentityMapAt(c, "placedDevice")["uuid"] = strings.Repeat("b", 32) }},
		{"component-owner-conflict", func(c map[string]any) { typedIdentityMapAt(c, "component")["uuid"] = strings.Repeat("e", 16) }},
		{"device-library-conflict", func(c map[string]any) { typedIdentityMapAt(c, "device")["libraryUuid"] = "other-library" }},
		{"placed-library-conflict", func(c map[string]any) { typedIdentityMapAt(c, "placedDevice")["libraryUuid"] = "other-library" }},
		{"footprint-library-domain", func(c map[string]any) { typedIdentityMapAt(c, "footprint")["uuid"] = strings.Repeat("c", 32) }},
		{"source-instance-conflict", func(c map[string]any) {
			typedIdentityMapAt(c, "deviceResolution", "footprintSource")["instanceUuid"] = strings.Repeat("e", 16)
		}},
		{"source-instance-domain", func(c map[string]any) {
			typedIdentityMapAt(c, "deviceResolution", "footprintSource")["uuid"] = strings.Repeat("d", 16)
		}},
		{"source-nonhex", func(c map[string]any) {
			typedIdentityMapAt(c, "deviceResolution", "footprintSource")["uuid"] = strings.Repeat("z", 32)
		}},
		{"source-library-conflict", func(c map[string]any) {
			typedIdentityMapAt(c, "deviceResolution", "footprintSource")["libraryUuid"] = "other-library"
		}},
		{"source-kind", func(c map[string]any) {
			typedIdentityMapAt(c, "deviceResolution", "footprintSource")["sourceKind"] = "unproven"
		}},
		{"false-footprint-proof", func(c map[string]any) { typedIdentityMapAt(c, "deviceResolution")["sameFootprintUUID"] = false }},
		{"unknown-proof", func(c map[string]any) { typedIdentityMapAt(c, "deviceResolution")["via"] = "unknown" }},
		{"legacy-short-proof", func(c map[string]any) { typedIdentityMapAt(c, "deviceResolution")["via"] = "lcsc-footprint-source" }},
		{"supplier-conflict", func(c map[string]any) { c["supplierId"] = "C999999" }},
		{"supplier-malformed", func(c map[string]any) { c["supplierId"] = "not-a-C-number" }},
		{"custom-invented-lcsc", func(c map[string]any) { c["supplierId"] = "" }},
		{"lcsc-conflict", func(c map[string]any) { typedIdentityMapAt(c, "deviceResolution")["lcsc"] = "C999999" }},
		{"missing-model-and-name", func(c map[string]any) {
			delete(c, "manufacturerId")
			c["name"] = ""
			delete(typedIdentityMapAt(c, "component"), "name")
		}},
		{"dynamic-name-equals-prefix", func(c map[string]any) {
			delete(c, "manufacturerId")
			c["name"] = "=computed"
			typedIdentityMapAt(c, "component")["name"] = "=computed"
		}},
		{"active-identity-error", func(c map[string]any) { c["deviceIdentityError"] = "unresolved exact identity" }},
		{"active-compatibility-error", func(c map[string]any) { c["deviceIdentityCompatibilityError"] = "unsupported exact identity" }},
		{"empty-identity-error", func(c map[string]any) { c["deviceIdentityError"] = "" }},
		{"null-identity-error", func(c map[string]any) { c["deviceIdentityError"] = nil }},
		{"empty-compatibility-error", func(c map[string]any) { c["deviceIdentityCompatibilityError"] = "" }},
		{"null-compatibility-error", func(c map[string]any) { c["deviceIdentityCompatibilityError"] = nil }},
	}
	for _, path := range [][]string{{"device", "libraryUuid"}, {"placedDevice", "libraryUuid"}, {"footprint", "libraryUuid"}, {"deviceResolution", "footprintSource", "libraryUuid"}} {
		for _, value := range []string{"", " ", "bad scope", "bad|scope", "bad\tscope", "bad\nscope"} {
			p, v := append([]string(nil), path...), value
			cases = append(cases, struct {
				name   string
				change func(map[string]any)
			}{
				name:   strings.Join(p, "/") + "/" + fmt.Sprintf("%q", v),
				change: func(c map[string]any) { typedIdentityMapAt(c, p[:len(p)-1]...)[p[len(p)-1]] = v },
			})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := typedIdentityFixture(false)
			tc.change(c)
			if err := validateSchematicTypedDeviceIdentity(c); err == nil {
				t.Fatal("conflicting or unsupported identity accepted")
			}
			if _, err := measuredSchematicDevice("U1", c); err == nil {
				t.Fatal("conflicting or unsupported identity remained replayable")
			}
		})
	}
}

func TestSchematicTypedIdentityReadPreserves67MixedPartsAndRawBytes(t *testing.T) {
	parts := make([]map[string]any, 67)
	for i := range parts {
		c := typedIdentityFixture(i%3 == 1)
		c["primitiveId"], c["designator"] = fmt.Sprintf("synthetic-%02d", i), fmt.Sprintf("U%d", i+1)
		if i%3 == 2 {
			// Direct native library identities require no recovery proof.
			delete(c, "placedDevice")
			delete(c, "deviceResolution")
		}
		parts[i] = c
	}
	body := typedIdentityBytes(t, typedIdentityEnvelope(parts...))
	before := bytes.Clone(body)
	cfg, server, cleanup := newBlockApplyTestDaemon(t, func(call blockApplyTestCall) string {
		t.Errorf("pure read validation dispatched unexpected action %s", call.Action)
		return `{"ok":false,"error":{"message":"no extra actions allowed"}}`
	})
	defer cleanup()
	cfg.project, cfg.doc = "project", "page"
	got, err := validateSchematicIdentityRead(cfg, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) || !bytes.Equal(body, before) {
		t.Fatal("complete typed identity read did not preserve original response bytes")
	}
	if calls := server.snapshot(); len(calls) != 0 {
		t.Fatalf("pure validation performed additional actions: %+v", calls)
	}
}

func TestSchematicTypedIdentityReadDispatchesOnlyTheRequestedList(t *testing.T) {
	body := typedIdentityBytes(t, typedIdentityEnvelope(typedIdentityFixture(false), typedIdentityFixture(true)))
	cfg, server, cleanup := newBlockApplyTestDaemon(t, func(call blockApplyTestCall) string {
		if call.Action != "schematic.components.list" {
			t.Errorf("unexpected compatibility action %s", call.Action)
			return `{"ok":false,"error":{"message":"no compatibility probe allowed"}}`
		}
		return string(body)
	})
	defer cleanup()
	if _, err := requestAction(cfg, "schematic.components.list", "w1", map[string]any{"includeDeviceIdentity": true}); err != nil {
		t.Fatal(err)
	}
	if calls := server.snapshot(); len(calls) != 1 || calls[0].Action != "schematic.components.list" {
		t.Fatalf("expected one typed list and no compatibility probe, got %+v", calls)
	}
}

func TestSchematicTypedIdentityReadAnnotatesUnsupportedProofWithoutNetwork(t *testing.T) {
	for _, scenario := range []string{"unresolved-instance", "legacy-lcsc-proof", "legacy-scoped-proof", "missing-proof", "native-owner-conflict", "existing-identity-error"} {
		t.Run(scenario, func(t *testing.T) {
			valid, invalid := typedIdentityFixture(false), typedIdentityFixture(false)
			switch scenario {
			case "unresolved-instance":
				invalid["device"] = invalid["placedDevice"]
				delete(invalid, "placedDevice")
				delete(invalid, "deviceResolution")
			case "legacy-lcsc-proof":
				typedIdentityMapAt(invalid, "deviceResolution")["via"] = "lcsc-footprint-source"
			case "legacy-scoped-proof":
				invalid["supplierId"] = ""
				typedIdentityMapAt(invalid, "deviceResolution")["via"] = "scoped-footprint-source"
				delete(typedIdentityMapAt(invalid, "deviceResolution"), "lcsc")
			case "missing-proof":
				delete(invalid, "deviceResolution")
			case "native-owner-conflict":
				typedIdentityMapAt(invalid, "component")["uuid"] = strings.Repeat("e", 16)
			case "existing-identity-error":
				invalid["deviceIdentityError"] = "original connector failure"
			}
			body := typedIdentityBytes(t, typedIdentityEnvelope(valid, invalid))
			original := bytes.Clone(body)
			cfg, server, cleanup := newBlockApplyTestDaemon(t, func(call blockApplyTestCall) string {
				t.Errorf("unsupported proof triggered network action %s", call.Action)
				return `{"ok":false}`
			})
			defer cleanup()
			cfg.project, cfg.doc = "project", "page"
			got, err := validateSchematicIdentityRead(cfg, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, body)
			if err != nil {
				t.Fatalf("unsupported identity should retain the response and annotate its part: %v", err)
			}
			parsed := typedIdentityDecode(t, got)
			rows := parsed["result"].(map[string]any)["components"].([]any)
			bad := rows[1].(map[string]any)
			problem, ok := bad["deviceIdentityCompatibilityError"].(string)
			if !ok || !strings.Contains(strings.ToLower(problem), "unsupported") {
				t.Fatalf("unsupported proof lacks an explicit unsupported diagnostic: %+v", bad)
			}
			if _, err := measuredSchematicDevice("U2", bad); err == nil {
				t.Fatal("unsupported identity remained replayable")
			}
			delete(bad, "deviceIdentityCompatibilityError")
			if !reflect.DeepEqual(bad, invalid) || !reflect.DeepEqual(rows[0], valid) {
				t.Fatal("validation altered original identity evidence or the valid neighboring part")
			}
			if !bytes.Equal(body, original) || parsed["id"] != "synthetic-identity-read" || parsed["sequence"] != float64(42) {
				t.Fatal("validation mutated input bytes or discarded envelope provenance")
			}
			if calls := server.snapshot(); len(calls) != 0 {
				t.Fatalf("unsupported read invoked additional actions: %+v", calls)
			}
		})
	}
}

func TestSchematicTypedIdentityReadRejectsMissingOrConflictingContext(t *testing.T) {
	for _, scenario := range []string{"missing-context", "null-context", "missing-project", "missing-document", "missing-document-kind", "wrong-project-type", "wrong-document-type", "wrong-document-kind", "project-conflict", "document-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			envelope := typedIdentityEnvelope(typedIdentityFixture(false))
			context := envelope["context"].(map[string]any)
			context["documentUuid"] = strings.Repeat("1", 16)
			switch scenario {
			case "missing-context":
				delete(envelope, "context")
			case "null-context":
				envelope["context"] = nil
			case "missing-project":
				delete(context, "projectUuid")
			case "missing-document":
				delete(context, "documentUuid")
			case "missing-document-kind":
				delete(context, "documentType")
			case "wrong-project-type":
				context["projectUuid"] = []any{"project"}
			case "wrong-document-type":
				context["documentUuid"] = []any{"page"}
			case "wrong-document-kind":
				context["documentType"] = "pcb"
			case "project-conflict":
				context["projectUuid"] = "other-project"
			case "document-conflict":
				context["documentUuid"] = "other-page"
			}
			body := typedIdentityBytes(t, envelope)
			original := bytes.Clone(body)
			got, err := validateSchematicIdentityRead(&appConfig{project: "project", doc: strings.Repeat("1", 16)}, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, body)
			if err == nil {
				result := typedIdentityDecode(t, got)["result"].(map[string]any)
				part := result["components"].([]any)[0].(map[string]any)
				if _, identityErr := measuredSchematicDevice("U1", part); identityErr == nil {
					t.Fatal("unbound or mismatched response context retained an accepted identity")
				}
			}
			if !bytes.Equal(body, original) {
				t.Fatal("context rejection mutated caller response bytes")
			}
		})
	}
}

func TestSchematicTypedIdentityReadAcceptsProjectNameAndDocumentSelector(t *testing.T) {
	for _, scenario := range []string{"project-uuid", "project-name", "document-16-uuid", "document-32-uuid", "document-name"} {
		t.Run(scenario, func(t *testing.T) {
			envelope := typedIdentityEnvelope(typedIdentityFixture(false))
			context := envelope["context"].(map[string]any)
			context["projectName"] = "Synthetic project"
			cfg := &appConfig{project: "project"}
			switch scenario {
			case "project-name":
				cfg.project = "Synthetic project"
			case "document-16-uuid":
				cfg.doc = strings.Repeat("1", 16)
				context["documentUuid"] = cfg.doc
			case "document-32-uuid":
				cfg.doc = strings.Repeat("1", 32)
				context["documentUuid"] = cfg.doc
			case "document-name":
				// Named selectors are guarded by ensureActiveDoc and the connector.
				cfg.doc = "Synthetic schematic"
			}
			body := typedIdentityBytes(t, envelope)
			got, err := validateSchematicIdentityRead(cfg, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, body)
			if err != nil || !bytes.Equal(got, body) {
				t.Fatalf("valid context selector was rejected or rewrote raw evidence: %v", err)
			}
		})
	}
}

func TestSchematicTypedIdentityReadPreservesExistingErrorValues(t *testing.T) {
	for _, key := range []string{"deviceIdentityError", "deviceIdentityCompatibilityError"} {
		for _, value := range []any{nil, "", "original unresolved proof"} {
			t.Run(key+"/"+fmt.Sprint(value), func(t *testing.T) {
				c := typedIdentityFixture(false)
				c[key] = value
				body := typedIdentityBytes(t, typedIdentityEnvelope(c))
				got, err := validateSchematicIdentityRead(&appConfig{}, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, body)
				if err != nil {
					t.Fatal(err)
				}
				part := typedIdentityDecode(t, got)["result"].(map[string]any)["components"].([]any)[0].(map[string]any)
				actual, exists := part[key]
				if !exists || !reflect.DeepEqual(actual, value) {
					t.Fatal("existing identity diagnostic was removed or replaced")
				}
				if _, err := measuredSchematicDevice("U1", part); err == nil {
					t.Fatal("existing identity error, including empty/null, remained replayable")
				}
			})
		}
	}
}

func TestSchematicTypedIdentityReadLeavesUnrequestedAndFailedResponsesUntouched(t *testing.T) {
	for _, scenario := range []string{"different-action", "unrequested", "false", "mistyped-option", "failed-envelope"} {
		t.Run(scenario, func(t *testing.T) {
			envelope := typedIdentityEnvelope(typedIdentityFixture(false))
			delete(envelope, "context")
			action, payload := "schematic.components.list", map[string]any{"includeDeviceIdentity": true}
			switch scenario {
			case "different-action":
				action = "schematic.component.inspect"
			case "unrequested":
				payload = map[string]any{}
			case "false":
				payload["includeDeviceIdentity"] = false
			case "mistyped-option":
				payload["includeDeviceIdentity"] = "true"
			case "failed-envelope":
				envelope["ok"] = false
			}
			body := typedIdentityBytes(t, envelope)
			got, err := validateSchematicIdentityRead(&appConfig{}, action, payload, body)
			if err != nil || !bytes.Equal(got, body) {
				t.Fatalf("unrequested or failed response changed: %v", err)
			}
		})
	}
}

func TestSchematicTypedIdentityDirectLibraryKeepsContextAndScope(t *testing.T) {
	for _, scenario := range []string{"missing-context", "wrong-project", "invalid-scope", "whitespace-scope"} {
		t.Run(scenario, func(t *testing.T) {
			c := typedIdentityFixture(true)
			delete(c, "placedDevice")
			delete(c, "deviceResolution")
			envelope := typedIdentityEnvelope(c)
			cfg := &appConfig{project: "project"}
			switch scenario {
			case "missing-context":
				delete(envelope, "context")
			case "wrong-project":
				schematicIdentityMap(envelope, "context")["projectUuid"] = "other"
			case "invalid-scope":
				schematicIdentityMap(c, "device")["libraryUuid"] = "bad|scope"
			case "whitespace-scope":
				schematicIdentityMap(c, "device")["libraryUuid"] = "bad scope"
			}
			body, err := validateSchematicIdentityRead(cfg, "schematic.components.list", map[string]any{"includeDeviceIdentity": true}, typedIdentityBytes(t, envelope))
			if err != nil {
				t.Fatal(err)
			}
			got := schematicIdentityMap(typedIdentityDecode(t, body), "result")["components"].([]any)[0].(map[string]any)
			if _, err := measuredSchematicDevice("U1", got); err == nil {
				t.Fatal("direct identity ignored context/scope failure")
			}
			if strings.Contains(scenario, "scope") {
				if _, err := measuredSchematicDevice("U1", c); err == nil {
					t.Fatal("offline direct identity accepted an invalid library scope")
				}
			}
		})
	}
}

func TestSchematicTypedSymbolOnlyIdentity(t *testing.T) {
	fixture := func() map[string]any {
		c := typedIdentityFixture(true)
		device := schematicIdentityMap(c, "device")
		placed := schematicIdentityMap(c, "placedDevice")
		c["footprint"] = map[string]any{"uuid": ""}
		c["symbol"] = map[string]any{"uuid": strings.Repeat("e", 16)}
		c["deviceResolution"] = map[string]any{"via": "native-symbol-only-device-source", "footprintAbsent": true, "symbolOnlySource": map[string]any{"instanceUuid": placed["uuid"], "uuid": device["uuid"], "libraryUuid": device["libraryUuid"], "sourceKind": "project-epro2", "footprintInstanceUuid": "", "symbolInstanceUuid": strings.Repeat("e", 16)}}
		return c
	}
	if err := validateSchematicTypedDeviceIdentity(fixture()); err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{{"deviceResolution", "footprintAbsent"}, {"deviceResolution", "symbolOnlySource", "uuid"}, {"deviceResolution", "symbolOnlySource", "sourceKind"}, {"deviceResolution", "symbolOnlySource", "footprintInstanceUuid"}, {"footprint", "uuid"}, {"symbol", "uuid"}, {"placedDevice", "uuid"}, {"manufacturerId"}} {
		c := fixture()
		owner := typedIdentityMapAt(c, path[:len(path)-1]...)
		delete(owner, path[len(path)-1])
		if err := validateSchematicTypedDeviceIdentity(c); err == nil {
			t.Fatalf("missing proof accepted: %v", path)
		}
	}
	c := fixture()
	typedIdentityMapAt(c, "footprint")["uuid"] = strings.Repeat("d", 16)
	if err := validateSchematicTypedDeviceIdentity(c); err == nil {
		t.Fatal("unexpected footprint accepted")
	}
}
