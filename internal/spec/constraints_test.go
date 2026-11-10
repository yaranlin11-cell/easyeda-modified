package spec

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func num(v float64) *float64 { return &v }
func constraintFixture(t *testing.T) (ConstraintSet, MeasurementSet, string) {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "source.txt")
	if e := os.WriteFile(p, []byte("synthetic requirements and independent measurement"), 0600); e != nil {
		t.Fatal(e)
	}
	h, e := FileSHA(p)
	if e != nil {
		t.Fatal(e)
	}
	s := []GraphSource{{ID: "s", Path: "source.txt", SHA256: h}}
	proof := EvidenceRef{SourceID: "s", Locator: "synthetic fixture"}
	w := ConstraintSet{SchemaVersion: 1, Sources: s, Rules: []NumericConstraint{{ID: "hole", Scope: "carrier", Subject: "minimum-through-via", Metric: "hole-diameter", Unit: "um", Stage: "design", Mode: "min", Min: num(200), Evidence: proof}}}
	g := MeasurementSet{SchemaVersion: 1, Sources: s, Measurements: []ConstraintMeasurement{{RuleID: "hole", Scope: "carrier", Subject: "minimum-through-via", Metric: "hole-diameter", Unit: "mm", Stage: "design", Value: num(0.25), Evidence: proof}}}
	return w, g, d
}
func TestConstraintComparison(t *testing.T) {
	cases := []struct {
		name, status string
		edit         func(*ConstraintSet, *MeasurementSet)
	}{
		{"larger than minimum", "pass", func(w *ConstraintSet, g *MeasurementSet) {}},
		{"boundary converted", "pass", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Value = num(0.2) }},
		{"undersize", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Value = num(0.199) }},
		{"missing", "incomplete", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements = nil }},
		{"null", "incomplete", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Value = nil }},
		{"wrong scope", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Scope = "module" }},
		{"wrong subject", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Subject = "mounting-hole" }},
		{"wrong metric", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Metric = "pad-diameter" }},
		{"CAD cannot prove fabrication", "fail", func(w *ConstraintSet, g *MeasurementSet) { w.Rules[0].Stage = "fabricated" }},
		{"incompatible units", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Unit = "percent" }},
		{"typo unit", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Unit = "mils" }},
		{"duplicate", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements = append(g.Measurements, g.Measurements[0]) }},
		{"unknown rule", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].RuleID = "other" }},
		{"invalid bounds", "fail", func(w *ConstraintSet, g *MeasurementSet) { w.Rules[0].Mode = "range"; w.Rules[0].Max = num(190) }},
		{"range upper boundary", "pass", func(w *ConstraintSet, g *MeasurementSet) { w.Rules[0].Mode = "range"; w.Rules[0].Max = num(250) }},
		{"range high", "fail", func(w *ConstraintSet, g *MeasurementSet) { w.Rules[0].Mode = "range"; w.Rules[0].Max = num(249) }},
		{"maximum", "pass", func(w *ConstraintSet, g *MeasurementSet) {
			w.Rules[0].Mode = "max"
			w.Rules[0].Min = nil
			w.Rules[0].Max = num(250)
		}},
		{"nonfinite", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Value = num(math.NaN()) }},
		{"negative length", "fail", func(w *ConstraintSet, g *MeasurementSet) { w.Rules[0].Min = num(-1) }},
		{"fractional layer count", "fail", func(w *ConstraintSet, g *MeasurementSet) {
			w.Rules[0].Unit = "count"
			w.Rules[0].Min = num(10)
			g.Measurements[0].Unit = "count"
			g.Measurements[0].Value = num(10.5)
		}},
		{"invalid percentage", "fail", func(w *ConstraintSet, g *MeasurementSet) {
			w.Rules[0].Unit = "percent"
			w.Rules[0].Min = num(110)
			g.Measurements[0].Unit = "percent"
			g.Measurements[0].Value = num(120)
		}},
		{"missing provenance", "fail", func(w *ConstraintSet, g *MeasurementSet) { g.Measurements[0].Evidence.Locator = "" }},
		{"changed hash", "fail", func(w *ConstraintSet, g *MeasurementSet) {
			g.Sources = append([]GraphSource(nil), g.Sources...)
			g.Sources[0].SHA256 = "bad"
		}},
		{"reference alone not acceptance", "incomplete", func(w *ConstraintSet, g *MeasurementSet) {
			w.Rules[0].Mode = "reference"
			w.Rules[0].Min = nil
			g.Measurements = nil
		}},
		{"failure dominates missing", "fail", func(w *ConstraintSet, g *MeasurementSet) { w.SchemaVersion = 2; g.Measurements = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, g, d := constraintFixture(t)
			tc.edit(&w, &g)
			r := CheckConstraints(w, g, d, d)
			if r.Status != tc.status {
				t.Fatalf("%+v", r)
			}
		})
	}
}
