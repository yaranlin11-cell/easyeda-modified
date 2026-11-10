package app

import (
	"encoding/json"
	"testing"
)

func TestSchComposeProtectedPagePinInventory(t *testing.T) {
	_, env := composeApplyFixture(t, true)
	result := env["result"].(map[string]any)
	page := result["pagePrimitives"].(map[string]any)
	var owner string
	for _, raw := range result["components"].([]any) {
		c := raw.(map[string]any)
		if c["componentType"] == "part" {
			owner = c["primitiveId"].(string)
			break
		}
	}
	page["pins"] = []any{map[string]any{"primitiveId": "owned-pin", "componentId": owner}}
	page["attributes"] = []any{map[string]any{"primitiveId": "pin-attribute", "X": 0, "Y": 0, "Rotation": 0, "Color": nil, "FontName": nil, "FontSize": nil, "Bold": nil, "Italic": nil, "UnderLine": nil, "AlignMode": nil, "FillColor": nil, "Key": "PinName", "Value": "IO", "KeyVisible": false, "ValueVisible": true, "ParentPrimitiveId": "owned-pin"}}
	if _, err := schComposeProtectedPage(result); err != nil {
		t.Fatal(err)
	}
	if err := schComposeOrdinaryClearable(page); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing-pins", func(p map[string]any) { delete(p, "pins") }},
		{"unknown-class", func(p map[string]any) { p["unknown"] = []any{} }},
		{"missing-owner", func(p map[string]any) { delete(p["pins"].([]any)[0].(map[string]any), "componentId") }},
		{"unknown-owner", func(p map[string]any) { p["pins"].([]any)[0].(map[string]any)["componentId"] = "absent" }},
		{"sheet-owner", func(p map[string]any) { p["pins"].([]any)[0].(map[string]any)["componentId"] = "sheet" }},
		{"duplicate-pin", func(p map[string]any) { p["pins"] = append(p["pins"].([]any), p["pins"].([]any)[0]) }},
		{"component-id-collision", func(p map[string]any) { p["pins"].([]any)[0].(map[string]any)["primitiveId"] = owner }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(result)
			var clone map[string]any
			_ = json.Unmarshal(raw, &clone)
			tc.mutate(clone["pagePrimitives"].(map[string]any))
			if _, err := schComposeProtectedPage(clone); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
	page["attributes"].([]any)[0].(map[string]any)["ParentPrimitiveId"] = "unknown-parent"
	if err := schComposeOrdinaryClearable(page); err == nil {
		t.Fatal("orphan attribute accepted")
	}
}
