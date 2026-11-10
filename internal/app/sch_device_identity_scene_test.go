package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Historical full CLI proofs still have a stable scene representation. Build
// archived evidence directly; current reads must never recreate it with debug.
func identityCompatResolvedSceneFixture(t *testing.T, custom ...bool) map[string]any {
	t.Helper()
	original, hit := identityCompatFixture()
	if len(custom) != 0 && custom[0] {
		original, hit, _ = identityCompatCustomFixture("MOTOBOX-TP-1MM", false)
	}
	original["deviceIdentityCandidates"] = []any{map[string]any{"footprintLibraryUuid": "old-personal-copy"}}
	original["placedDevice"] = original["device"]
	original["device"] = map[string]any{"uuid": hit.UUID, "libraryUuid": hit.LibraryUUID, "name": original["name"]}
	r := map[string]any{
		"via": "lcsc-footprint-source", "resolver": "cli-legacy-placed-footprint-v1", "lcsc": hit.SupplierID,
		"sameFootprintUUID": true, "instanceFootprint": original["footprint"],
		"libraryFootprint": map[string]any{"uuid": hit.Device.Footprint.UUID, "libraryUuid": hit.Device.Footprint.LibraryUUID},
		"footprintSource":  map[string]any{"instanceUuid": "abe23dba1def1246", "uuid": identityCompatSource().UUID, "libraryUuid": hit.Device.Footprint.LibraryUUID},
		"probeId":          "historical-probe", "connectorError": original["deviceIdentityError"],
	}
	if hit.SupplierID == "" {
		r["via"] = "scoped-library-footprint-source"
		delete(r, "lcsc")
	}
	original["deviceResolution"] = r
	delete(original, "deviceIdentityError")
	return map[string]any{"components": []any{original}, "wires": []any{}, "connectivitySummary": map[string]any{"netCount": float64(1)}}
}

func typedIdentitySceneRead(t *testing.T, change func(map[string]any)) map[string]any {
	t.Helper()
	original := typedIdentityFixture(false)
	if change != nil {
		change(original)
	}
	rawBefore, _ := json.Marshal(original)
	cfg, daemon, cleanup := newBlockApplyTestDaemon(t, func(call blockApplyTestCall) string {
		if call.Action != "schematic.components.list" {
			t.Fatalf("unexpected identity action %s", call.Action)
		}
		return identityCompatEnvelope(map[string]any{"components": []any{original}, "wires": []any{}, "connectivitySummary": map[string]any{"netCount": 1}}, "page")
	})
	defer cleanup()
	runner := applyRunner{cfg: cfg, window: "w1"}
	result, err := runner.runAction("schematic.components.list", map[string]any{"includeDeviceIdentity": true}, defaultActionTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(daemon.snapshot()) != 1 {
		t.Fatal("typed scene identity triggered an extra action")
	}
	rawAfter, _ := json.Marshal(original)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("scene read modified its source")
	}
	return result.(map[string]any)
}

func TestSchematicTypedIdentityScenePreservesFreshProof(t *testing.T) {
	before := typedIdentitySceneRead(t, nil)
	wanted, err := schDesignatorScene(before)
	if err != nil {
		t.Fatal(err)
	}
	wanted = identityCompatCloneScene(t, wanted)
	live := typedIdentitySceneRead(t, nil)
	if err := checkSchPreservedSource(wanted, live); err != nil {
		t.Fatal(err)
	}
	got, err := schDesignatorScene(live)
	if err != nil || !reflect.DeepEqual(wanted, got) {
		t.Fatal("unchanged typed native proof changed its scene")
	}
	c := live["components"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(c, typedIdentityFixture(false)) {
		t.Fatal("fresh typed evidence was rewritten")
	}
}

func TestSchematicTypedIdentitySceneKeepsIdentityAndCircuitDrift(t *testing.T) {
	before := typedIdentitySceneRead(t, nil)
	wanted, err := schDesignatorScene(before)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"device", func(c map[string]any) { schematicIdentityMap(c, "device")["uuid"] = strings.Repeat("e", 32) }},
		{"source", func(c map[string]any) {
			schematicIdentityMap(schematicIdentityMap(c, "deviceResolution"), "footprintSource")["uuid"] = strings.Repeat("f", 32)
		}},
		{"instance", func(c map[string]any) { schematicIdentityMap(c, "footprint")["uuid"] = strings.Repeat("e", 16) }},
		{"missing-source", func(c map[string]any) { delete(schematicIdentityMap(c, "deviceResolution"), "footprintSource") }},
		{"legacy-proof", func(c map[string]any) { schematicIdentityMap(c, "deviceResolution")["via"] = "lcsc-footprint-source" }},
		{"position", func(c map[string]any) { c["x"] = float64(101) }},
		{"property", func(c map[string]any) { c["otherProperty"] = map[string]any{"Value": "changed"} }},
		{"pin-net", func(c map[string]any) { c["pins"].([]any)[0].(map[string]any)["net"] = "changed" }},
		{"pin-nc", func(c map[string]any) { c["pins"].([]any)[0].(map[string]any)["noConnected"] = true }},
		{"error", func(c map[string]any) { c["deviceIdentityError"] = "typed lookup failed" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			live := typedIdentitySceneRead(t, tc.change)
			if err := checkSchPreservedSource(wanted, live); err == nil {
				t.Fatal("identity or circuit drift was ignored")
			}
		})
	}
}

func identityCompatCloneScene(t *testing.T, scene map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(scene)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSchematicIdentityCompatSceneIgnoresOnlySupersededDiagnostics(t *testing.T) {
	original := identityCompatResolvedSceneFixture(t)
	rawBefore, _ := json.Marshal(original)
	wanted, err := schDesignatorScene(original)
	if err != nil {
		t.Fatal(err)
	}
	live := identityCompatCloneScene(t, original)
	c := live["components"].([]any)[0].(map[string]any)
	c["deviceIdentityCandidates"] = []any{map[string]any{"footprintLibraryUuid": "different-rejected-copy"}, map[string]any{"uuid": "another-rejected-copy"}}
	r := schematicIdentityMap(c, "deviceResolution")
	r["probeId"], r["connectorError"] = "new-fresh-probe", "same failure with reordered candidates"
	if err := checkSchPreservedSource(wanted, live); err != nil {
		t.Fatalf("fresh exact identity rejected volatile old diagnostics: %v", err)
	}
	rawAfter, _ := json.Marshal(original)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("scene normalization modified raw identity evidence")
	}
	for _, scenario := range []string{"device", "device-library", "instance-footprint", "instance-footprint-library", "footprint-name", "native-source", "native-source-library", "association", "association-library", "property", "position", "rotation", "pin-net", "pin-position", "assembly", "active-identity-error", "active-compat-error"} {
		t.Run(scenario, func(t *testing.T) {
			live := identityCompatCloneScene(t, original)
			c := live["components"].([]any)[0].(map[string]any)
			r := schematicIdentityMap(c, "deviceResolution")
			switch scenario {
			case "device":
				schematicIdentityMap(c, "device")["uuid"] = strings.Repeat("a", 32)
			case "device-library":
				schematicIdentityMap(c, "device")["libraryUuid"] = "different"
			case "instance-footprint":
				schematicIdentityMap(c, "footprint")["uuid"] = strings.Repeat("b", 16)
			case "instance-footprint-library":
				schematicIdentityMap(c, "footprint")["libraryUuid"] = "different"
			case "footprint-name":
				schematicIdentityMap(c, "footprint")["name"] = "different"
			case "native-source":
				schematicIdentityMap(r, "footprintSource")["uuid"] = strings.Repeat("c", 32)
			case "native-source-library":
				schematicIdentityMap(r, "footprintSource")["libraryUuid"] = "different"
			case "association":
				schematicIdentityMap(r, "libraryFootprint")["uuid"] = strings.Repeat("d", 32)
			case "association-library":
				schematicIdentityMap(r, "libraryFootprint")["libraryUuid"] = "different"
			case "property":
				c["otherProperty"] = map[string]any{"Value": "different"}
			case "position":
				c["x"] = 191.0
			case "rotation":
				c["rotation"] = 90.0
			case "pin-net":
				c["pins"].([]any)[0].(map[string]any)["net"] = "different"
			case "pin-position":
				c["pins"].([]any)[0].(map[string]any)["x"] = 144.0
			case "assembly":
				c["addIntoBom"] = false
			case "active-identity-error":
				c["deviceIdentityError"] = "unresolved"
			case "active-compat-error":
				c["deviceIdentityCompatibilityError"] = "unresolved"
			}
			if err := checkSchPreservedSource(wanted, live); err == nil {
				t.Fatal("stable resolved identity or circuit drift was ignored")
			}
		})
	}
}

func TestSchematicIdentityCompatSceneCustomExactProof(t *testing.T) {
	original := identityCompatResolvedSceneFixture(t, true)
	wanted, err := schDesignatorScene(original)
	if err != nil {
		t.Fatal(err)
	}
	live := identityCompatCloneScene(t, original)
	c := live["components"].([]any)[0].(map[string]any)
	c["deviceIdentityCandidates"] = []any{map[string]any{"footprintLibraryUuid": "different-rejected-copy"}}
	schematicIdentityMap(c, "deviceResolution")["probeId"] = "fresh-custom-probe"
	if err := checkSchPreservedSource(wanted, live); err != nil {
		t.Fatalf("custom exact identity rejected volatile diagnostics: %v", err)
	}
}

func TestSchematicIdentityCompatSceneKeepsUnprovenDiagnostics(t *testing.T) {
	for _, scenario := range []string{"no-proof", "unknown-resolver", "missing-source", "conflicting-association", "active-error"} {
		t.Run(scenario, func(t *testing.T) {
			original := identityCompatResolvedSceneFixture(t)
			c := original["components"].([]any)[0].(map[string]any)
			r := schematicIdentityMap(c, "deviceResolution")
			switch scenario {
			case "no-proof":
				delete(c, "deviceResolution")
			case "unknown-resolver":
				r["resolver"] = "not-the-verified-cli-resolver"
			case "missing-source":
				delete(r, "footprintSource")
			case "conflicting-association":
				schematicIdentityMap(r, "libraryFootprint")["uuid"] = strings.Repeat("a", 32)
			case "active-error":
				c["deviceIdentityError"] = "unresolved"
			}
			wanted, err := schDesignatorScene(original)
			if err != nil {
				t.Fatal(err)
			}
			live := identityCompatCloneScene(t, original)
			live["components"].([]any)[0].(map[string]any)["deviceIdentityCandidates"] = []any{map[string]any{"footprintLibraryUuid": "different"}}
			if err := checkSchPreservedSource(wanted, live); err == nil {
				t.Fatal("unproven connector diagnostics silently discarded")
			}
		})
	}
}
