package connectivity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFromReadPreservesNativeIdentityAndFootprintReference(t *testing.T) {
	for _, tc := range []struct {
		name      string
		footprint any
		want      string
	}{
		{"native", map[string]any{"uuid": "footprint-uuid", "libraryUuid": "library-uuid", "name": "QFN", "revision": "r1"}, "footprint-uuid"},
		{"legacy", "legacy-footprint", "legacy-footprint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := boundComponent("U1", map[string]any{ComponentIDProperty: "authored-component"})
			source["uniqueId"] = "native-instance"
			source["footprint"] = tc.footprint
			doc, err := FromRead(map[string]any{"components": []any{source}})
			if err != nil {
				t.Fatal(err)
			}
			c := doc.Components[0]
			if c.ID != "authored-component" || c.UniqueID != "native-instance" || c.Footprint != tc.want {
				t.Fatalf("native identity/reference lost: %+v", c)
			}
			if native, ok := tc.footprint.(map[string]any); ok && !reflect.DeepEqual(c.FootprintRef, native) {
				t.Fatalf("native footprint fields lost: %+v", c.FootprintRef)
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var loaded Document
			if err := json.Unmarshal(raw, &loaded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(loaded.Components, doc.Components) {
				t.Fatalf("JSON roundtrip lost native identity: %+v", loaded.Components)
			}
		})
	}
}

func TestFromReadRejectsExplicitlyUnavailablePinNets(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag string
	}{{"document", "pinNetsAvailable"}, {"component", "pinNetsUnavailable"}, {"netlist", "netlistAvailable"}} {
		t.Run(tc.name, func(t *testing.T) {
			source := boundComponent("U1", nil)
			// A failed collector can leave null placeholders, which are not proof
			// of intentional NC or of a real unconnected pin.
			source["pins"].([]any)[0].(map[string]any)["net"] = nil
			read := map[string]any{"components": []any{source}}
			switch tc.flag {
			case "pinNetsAvailable":
				read[tc.flag] = false
			case "pinNetsUnavailable":
				source[tc.flag] = true
			case "netlistAvailable":
				source[tc.flag] = false
			}
			if _, err := FromRead(read); err == nil || !strings.Contains(err.Error(), "pin net evidence unavailable") {
				t.Fatalf("failed pin-net collection accepted: %v", err)
			}
		})
	}
	legacy := boundComponent("U1", nil)
	legacy["pins"].([]any)[0].(map[string]any)["net"] = nil
	if _, err := FromRead(map[string]any{"components": []any{legacy}}); err != nil {
		t.Fatalf("legacy null evidence compatibility changed: %v", err)
	}
	if empty, err := FromRead(map[string]any{"components": []any{}, "pinNetsAvailable": false}); err != nil || len(empty.Components) != 0 {
		t.Fatalf("known-empty inventory requires no pin nets: %+v %v", empty, err)
	}
	if _, err := FromRead(map[string]any{"components": []any{nil}, "pinNetsAvailable": false}); err == nil {
		t.Fatal("malformed inventory claimed known-empty")
	}
}

func TestFromReadRejectsAmbiguousPinNetAttribution(t *testing.T) {
	// This is the connector's response when different devices on separate pages
	// share a designator: netlist capture succeeds, but pin-to-net attribution is
	// ambiguous and each affected pin carries null instead of a definite net.
	var read map[string]any
	if err := json.Unmarshal([]byte(`{"pinNetsAvailable":true,"components":[{"componentType":"part","designator":"U1","netAmbiguous":true,"pinsAvailable":true,"netlistAvailable":true,"pins":[{"pinNumber":"1","net":null,"noConnected":false}]}]}`), &read); err != nil {
		t.Fatal(err)
	}
	doc, err := FromRead(read)
	if err == nil || !strings.Contains(err.Error(), "U1: pin net attribution ambiguous") {
		t.Fatalf("ambiguous attribution accepted: document=%+v error=%v", doc, err)
	}
	for _, issue := range doc.Issues {
		if issue.Code == "unconnected-pin" {
			t.Fatalf("ambiguous read became a design diagnosis: %+v", issue)
		}
	}

	component := read["components"].([]any)[0].(map[string]any)
	component["pins"].([]any)[0].(map[string]any)["net"] = "GND"
	component["netAmbiguous"] = false
	if _, err := FromRead(read); err != nil {
		t.Fatalf("explicit unambiguous attribution rejected: %v", err)
	}
	delete(component, "netAmbiguous")
	if _, err := FromRead(read); err != nil {
		t.Fatalf("legacy attribution without ambiguity flag rejected: %v", err)
	}
}
