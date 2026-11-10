package app

import (
	"encoding/json"
	"testing"
)

func TestPreservedTitleBlockProjectsOnlyDeclaredSheetChanges(t *testing.T) {
	p, env := preserveDrawingFixture(t)
	r := env["result"].(map[string]any)
	page := r["pagePrimitives"].(map[string]any)
	for _, item := range page["attributes"].([]any) {
		row := item.(map[string]any)
		row["Key"] = "Name" // same key on parts/pins must remain protected
		if row["ParentPrimitiveId"] == "sheet" {
			row["KeyVisible"], row["ValueVisible"] = nil, nil
		}
	}
	if err := json.Unmarshal([]byte(`{"Name":{"value":"New title","showTitle":false,"showValue":false}}`), &p.TitleBlock); err != nil {
		t.Fatal(err)
	}
	pb, err := schCompositionPlaybook(p, preserveDrawingBytes(t, env), true, true)
	if err != nil {
		t.Fatal(err)
	}
	_, before := composeStep(t, pb, "verify-source-before-reset")
	_, final := composeStep(t, pb, "verify-all-pins-nets-nc")
	old := before.ExpectSchematic.OwnedAttributes
	want := final.ExpectSchematic.OwnedAttributes
	if old.Attributes["sheet-attr"].(map[string]any)["KeyVisible"] != nil || old.Attributes["sheet-attr"].(map[string]any)["Value"] != "={Value}" {
		t.Fatal("title patch changed source guard")
	}
	for _, item := range page["attributes"].([]any) {
		row := item.(map[string]any)
		if row["ParentPrimitiveId"] == "sheet" {
			row["Value"], row["KeyVisible"], row["ValueVisible"] = "New title", false, false
		}
	}
	if err := want.check(r); err != nil {
		t.Fatalf("declared patch rejected: %v", err)
	}
	if err := old.check(r); err == nil {
		t.Fatal("source guard stopped protecting baseline")
	}
	for _, id := range []string{"part-attr", "pin-attr", "sheet-attr"} {
		for _, item := range page["attributes"].([]any) {
			row := item.(map[string]any)
			if row["primitiveId"] == id {
				prev := row["FontSize"]
				row["FontSize"] = 99.0
				if err := want.check(r); err == nil {
					t.Fatalf("unplanned %s change accepted", id)
				}
				row["FontSize"] = prev
			}
		}
	}
	delete(want.Attributes, "sheet-attr")
	if _, err := schAttributesAfterTitleBlock(want, r, p.TitleBlock); err == nil {
		t.Fatal("missing measured title accepted")
	}
}
