package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zhoushoujianwork/easyeda-agent/internal/connectivity"
)

func preserveUnavailableUnwiredFixture(t *testing.T) (*schCompositionPlan, map[string]any) {
	t.Helper()
	p, env := preserveComposeFixture(t)
	result := env["result"].(map[string]any)
	parts := []any{}
	for _, item := range result["components"].([]any) {
		part := item.(map[string]any)
		if part["componentType"] == "netflag" {
			continue
		}
		if part["componentType"] == "part" {
			part["netlistAvailable"] = false
			part["netlistError"] = "netlist export returned no file"
			for _, item := range part["pins"].([]any) {
				pin := item.(map[string]any)
				pin["net"], pin["noConnected"] = nil, false
			}
		}
		parts = append(parts, part)
	}
	result["components"], result["count"], result["wires"] = parts, len(parts), []any{}
	result["pinNetsAvailable"] = false
	result["connectivitySummary"] = map[string]any{"scope": "activePage", "wires": 0, "buses": 0, "netflags": 0, "netports": 0, "netlabels": 0, "shortSymbols": 0}
	composeFixturePageInventory(result)
	return p, env
}

func preserveSnapshotBytes(t *testing.T, env map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPreserveUnwiredConnectivityProjectionKeepsNativeEvidence(t *testing.T) {
	p, env := preserveUnavailableUnwiredFixture(t)
	result := env["result"].(map[string]any)
	before := preserveSnapshotBytes(t, env)
	doc, provenance, err := schComposeKnownUnconnectedConnectivity(before, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Components) != len(p.Connectivity.Components) || len(doc.Nets) != 0 || len(doc.Connections) != 0 {
		t.Fatalf("zero-drawing proof synthesized a connection or lost parts: %+v", doc)
	}
	pinCount := 0
	for _, c := range doc.Components {
		for _, pin := range c.Pins {
			pinCount++
			if pin.ConnectionState != "unconnected" || pin.NoConnected {
				t.Fatalf("independently proven open pin was lost: %+v", pin)
			}
		}
	}
	digest := sha256.Sum256(before)
	if provenance == nil || provenance.Basis != "complete-active-page-zero-drawing" || provenance.SnapshotSHA256 != hex.EncodeToString(digest[:]) || provenance.PhysicalPins != pinCount {
		t.Fatalf("source provenance missing or incorrect: %+v", provenance)
	}
	if !bytes.Equal(before, preserveSnapshotBytes(t, env)) {
		t.Fatal("projection changed the original snapshot, flags or null pin nets")
	}
	if _, err := connectivity.FromRead(result); err == nil {
		t.Fatal("ordinary FromRead accepted a failed native netlist")
	}
	if err := compareObserved(doc, result); err == nil {
		t.Fatal("ordinary connectivity checkpoint borrowed Compose's exception")
	}
	if _, err := parseSchDesignatorBaseline(before); err == nil {
		t.Fatal("ordinary designator baseline borrowed Compose's exception")
	}
	pb, err := schCompositionPlaybook(p, before, true, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	var loaded playbook
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	var observedEnvelope map[string]any
	if err := json.Unmarshal(before, &observedEnvelope); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"verify-source-before-reset", "verify-fully-unwired-before-move"} {
		_, step := composeStep(t, &loaded, id)
		if !reflect.DeepEqual(step.ExpectSchematic.SourceConnectivityProvenance, provenance) {
			t.Fatalf("%s lost machine-readable provenance during queue JSON: %+v", id, step.ExpectSchematic.SourceConnectivityProvenance)
		}
		scene, err := schDesignatorScene(result)
		wantScene, wantErr := json.Marshal(scene)
		gotScene, gotErr := json.Marshal(step.ExpectSchematic.SourceScene)
		if err != nil || wantErr != nil || gotErr != nil || !bytes.Equal(wantScene, gotScene) {
			t.Fatalf("%s lost original native errors/false/null evidence: %v", id, err)
		}
		if err := step.ExpectSchematic.check(observedEnvelope["result"], nil); err != nil {
			t.Fatalf("unchanged source no longer satisfies its raw guard: %v", err)
		}
	}
}

func TestPreserveUnwiredProjectionRejectsIncompleteInventories(t *testing.T) {
	checks := []struct {
		name   string
		change func(map[string]any)
	}{
		{"wire availability", func(r map[string]any) { delete(r, "wiresAvailable") }},
		{"wire inventory", func(r map[string]any) { delete(r, "wires") }},
		{"page inventory", func(r map[string]any) { delete(r, "pagePrimitives") }},
		{"component inventory", func(r map[string]any) { delete(r, "components") }},
		{"net availability declaration", func(r map[string]any) { delete(r, "pinNetsAvailable") }},
		{"NC evidence", func(r map[string]any) {
			delete(r["components"].([]any)[1].(map[string]any)["pins"].([]any)[0].(map[string]any), "noConnected")
		}},
		{"null net evidence", func(r map[string]any) {
			delete(r["components"].([]any)[1].(map[string]any)["pins"].([]any)[0].(map[string]any), "net")
		}},
		{"junction", func(r map[string]any) { r["components"].([]any)[1].(map[string]any)["componentType"] = "junction" }},
	}
	for _, key := range []string{"wires", "buses", "netflags", "netports", "netlabels", "shortSymbols"} {
		checks = append(checks, struct {
			name   string
			change func(map[string]any)
		}{"missing summary " + key, func(r map[string]any) { delete(r["connectivitySummary"].(map[string]any), key) }})
	}
	for _, key := range []string{"wires", "buses", "arcs", "circles", "rectangles", "polygons", "texts", "objects", "attributes"} {
		checks = append(checks, struct {
			name   string
			change func(map[string]any)
		}{"missing page class " + key, func(r map[string]any) { delete(r["pagePrimitives"].(map[string]any), key) }})
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			p, env := preserveUnavailableUnwiredFixture(t)
			result := env["result"].(map[string]any)
			tc.change(result)
			before := preserveSnapshotBytes(t, env)
			if _, provenance, err := schComposeKnownUnconnectedConnectivity(before, result); err == nil || provenance != nil {
				t.Fatalf("incomplete source obtained a derived open-pin claim: %+v %v", provenance, err)
			}
			if pb, err := schCompositionPlaybook(p, before, true, true); err == nil || pb != nil {
				t.Fatal("incomplete source obtained a protected writing queue")
			}
		})
	}
}
