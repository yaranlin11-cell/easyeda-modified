package app

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func antennaTestRegion(layer int, rules []int, rect [4]float64) pcbKeepRegion {
	return pcbKeepRegion{PrimitiveID: "keepout", Layer: layer, RuleTypes: rules, GeometryAvailable: true,
		Source: pointsPolygonSource([][2]float64{{rect[0], rect[1]}, {rect[2], rect[1]}, {rect[2], rect[3]}, {rect[0], rect[3]}})}
}

func TestAntennaRegionCoverageRejectsIncompleteProof(t *testing.T) {
	rect := [4]float64{2, 2, 8, 8}
	full := antennaTestRegion(pcbLayerMulti, []int{5, 6, 7, 8}, [4]float64{0, 0, 10, 10})
	for _, tc := range []struct {
		name   string
		change func(*pcbKeepRegion)
		want   bool
	}{
		{"complete polygon and all rules", func(*pcbKeepRegion) {}, true},
		{"mere bbox overlap", func(r *pcbKeepRegion) { r.Source = antennaTestRegion(12, nil, [4]float64{0, 0, 3, 3}).Source }, false},
		{"triangle bbox encloses rectangle", func(r *pcbKeepRegion) { r.Source = pointsPolygonSource([][2]float64{{0, 0}, {10, 0}, {0, 10}}) }, false},
		{"concave notch corners inside", func(r *pcbKeepRegion) {
			r.Source = pointsPolygonSource([][2]float64{{0, 0}, {10, 0}, {10, 10}, {6, 10}, {6, 5}, {4, 5}, {4, 10}, {0, 10}})
		}, false},
		{"self intersection", func(r *pcbKeepRegion) {
			r.Source = pointsPolygonSource([][2]float64{{0, 0}, {10, 10}, {0, 10}, {10, 0}})
		}, false},
		{"missing no-wires", func(r *pcbKeepRegion) { r.RuleTypes = []int{6, 7, 8} }, false},
		{"no-pours alone", func(r *pcbKeepRegion) { r.RuleTypes = []int{7} }, false},
		{"geometry unavailable", func(r *pcbKeepRegion) { r.GeometryAvailable = false }, false},
		{"missing region identity", func(r *pcbKeepRegion) { r.PrimitiveID = "" }, false},
		{"bbox without source", func(r *pcbKeepRegion) { r.Source = nil; r.BBox = &layoutBBox{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10} }, false},
		{"ARC unavailable", func(r *pcbKeepRegion) { r.Source = []any{0, 0, "L", 10, 0, "ARC", 90, 10, 10, "L", 0, 10, 0, 0} }, false},
		{"multiple contours", func(r *pcbKeepRegion) {
			r.Source = []any{full.Source, antennaTestRegion(12, nil, [4]float64{4, 4, 6, 6}).Source}
		}, false},
		{"open polygon", func(r *pcbKeepRegion) { r.Source = []any{0, 0, "L", 10, 0, 10, 10, 0, 10} }, false},
		{"non-finite polygon", func(r *pcbKeepRegion) { r.Source = []any{0, 0, "L", math.Inf(1), 0, 10, 10, 0, 10, 0, 0} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := full
			tc.change(&region)
			if got := antennaRegionCovers(rect, []int{5, 6, 7}, region); got != tc.want {
				t.Fatalf("covered=%v want=%v source=%v", got, tc.want, region.Source)
			}
		})
	}
	if !antennaRegionCovers([4]float64{0, 0, 10, 10}, []int{5, 6, 7}, full) {
		t.Fatal("matching polygon boundary must cover the requested rectangle")
	}
	if antennaRegionCovers(rect, nil, full) || antennaRegionCovers([4]float64{0, 0, 0, 10}, []int{7}, full) {
		t.Fatal("missing rules or degenerate rectangle cannot prove coverage")
	}
}

func TestParseAntennaRegionsRequiresReadableSource(t *testing.T) {
	source := antennaTestRegion(12, nil, [4]float64{0, 0, 10, 10}).Source
	for _, tc := range []struct {
		name                  string
		availability          any
		setAvailability, want bool
	}{
		{"create result omits availability", nil, false, true},
		{"list confirms availability", true, true, true},
		{"explicit unavailable overrides source", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := map[string]any{"primitiveId": "r1", "layer": 12, "ruleType": []any{5, 6, 7, 8}, "source": source}
			if tc.setAvailability {
				result["geometryAvailable"] = tc.availability
			}
			for _, input := range []map[string]any{result, {"regions": []any{result}}} {
				regions := parseAntennaKeepRegions(input)
				if len(regions) != 1 || regions[0].PrimitiveID != "r1" || regions[0].GeometryAvailable != tc.want || len(regions[0].RuleTypes) != 4 {
					t.Fatalf("parsed=%+v", regions)
				}
			}
		})
	}
	regions := parseAntennaKeepRegions(map[string]any{"regions": []any{map[string]any{"primitiveId": "bad", "geometryAvailable": true, "bbox": map[string]any{"minX": 0, "minY": 0, "maxX": 10, "maxY": 10}}}})
	if len(regions) != 1 || regions[0].GeometryAvailable {
		t.Fatal("availability true and bbox cannot replace polygon source")
	}
	for _, change := range []func(map[string]any){
		func(r map[string]any) { r["layer"] = 12.5 },
		func(r map[string]any) { r["ruleType"] = []any{5, 6, 7, 8.5} },
		func(r map[string]any) { r["ruleType"] = []any{5, 6, 7, "unknown"} },
		func(r map[string]any) { delete(r, "ruleType") },
	} {
		r := map[string]any{"primitiveId": "r", "layer": 12, "ruleType": []any{5, 6, 7, 8}, "source": source}
		change(r)
		parsed := parseAntennaKeepRegions(r)
		if len(parsed) != 1 || antennaRegionCovers([4]float64{2, 2, 8, 8}, []int{5, 6, 7}, parsed[0]) {
			t.Fatalf("invalid metadata must not yield a partial coverage proof: %+v", parsed)
		}
	}
}

func TestChipAntennaIdentityProtectsModuleFeedRules(t *testing.T) {
	for _, device := range []string{"ESP32-S3-WROOM-1", "WROVER", "ESP32-C3-MINI", "ESP8266", ""} {
		if isChipAntennaPart(device, 100, 60) {
			t.Fatalf("%q cannot become a chip due to a small bbox", device)
		}
	}
	for _, device := range []string{"2450AT18A100E", "ANT-SMD_L3.2-W1.6", "chip antenna", "ANT1-ceramic"} {
		if !isChipAntennaPart(device, 100, 60) {
			t.Fatalf("known chip %q not recognized", device)
		}
	}
	if isChipAntennaPart("ANTENNA", 700, 900) || isChipAntennaPart("ANTENNA", math.NaN(), 60) {
		t.Fatal("invalid/module-sized bbox cannot use chip feed exception")
	}
}

func TestAntennaLayerCoverageDoesNotFoldRules(t *testing.T) {
	rect := [4]float64{0, 0, 10, 10}
	module := []int{5, 6, 7}
	for _, tc := range []struct {
		name    string
		layers  int
		regions []pcbKeepRegion
		want    bool
	}{
		{"unknown layer count", 0, []pcbKeepRegion{antennaTestRegion(12, []int{5, 6, 7, 8}, rect)}, false},
		{"odd layer count", 3, []pcbKeepRegion{antennaTestRegion(12, []int{5, 6, 7, 8}, rect)}, false},
		{"out-of-range layer count", 34, []pcbKeepRegion{antennaTestRegion(12, []int{5, 6, 7, 8}, rect)}, false},
		{"two-layer top only", 2, []pcbKeepRegion{antennaTestRegion(1, module, rect)}, false},
		{"two-layer top and bottom", 2, []pcbKeepRegion{antennaTestRegion(1, module, rect), antennaTestRegion(2, module, rect)}, true},
		{"two-layer MULTI", 2, []pcbKeepRegion{antennaTestRegion(12, module, rect)}, true},
		{"four-layer top bottom and rule8", 4, []pcbKeepRegion{antennaTestRegion(1, []int{5, 6, 7, 8}, rect), antennaTestRegion(2, module, rect)}, false},
		{"four-layer MULTI without rule8", 4, []pcbKeepRegion{antennaTestRegion(12, module, rect)}, false},
		{"four-layer complete MULTI", 4, []pcbKeepRegion{antennaTestRegion(12, []int{5, 6, 7, 8}, rect)}, true},
		{"split rule subsets", 2, []pcbKeepRegion{antennaTestRegion(12, []int{5}, rect), antennaTestRegion(12, []int{6, 7}, rect)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(antennaCoverageMissing(rect, module, tc.regions, tc.layers)) == 0; got != tc.want {
				t.Fatalf("covered=%v want=%v", got, tc.want)
			}
		})
	}
	chip := antennaTestRegion(12, []int{6, 7, 8}, rect)
	if len(antennaCoverageMissing(rect, []int{6, 7}, []pcbKeepRegion{chip}, 4)) != 0 {
		t.Fatal("chip antenna must allow its feed without rule5 (#129)")
	}
	if len(antennaCoverageMissing(rect, nil, []pcbKeepRegion{chip}, 4)) == 0 {
		t.Fatal("missing antenna rules cannot be replaced with rule8 alone")
	}
}

func antennaContextTestComponent(chip bool) map[string]any {
	maxX, maxY, padX, padY, device := 700, 900, 350, 400, "ESP32-S3-WROOM-1"
	if chip {
		maxX, maxY, padX, padY, device = 100, 60, 50, 30, "2450AT18A100E"
	}
	return map[string]any{"primitiveId": "ant", "designator": "ANT1", "manufacturerId": device,
		"bbox": map[string]any{"minX": 0, "minY": 0, "maxX": maxX, "maxY": maxY},
		"pads": []any{map[string]any{"primitiveId": "pad", "padNumber": "1", "net": "RF", "layer": 1, "x": padX, "y": padY, "rotation": 0, "shape": []any{"ELLIPSE", 10, 10}}}}
}

func TestFetchAntennaContextUsesGeneratorGeometry(t *testing.T) {
	for _, chip := range []bool{false, true} {
		t.Run(map[bool]string{false: "module strip", true: "chip feed exception"}[chip], func(t *testing.T) {
			component := antennaContextTestComponent(chip)
			cfg := newClearanceCheckFixture(t, 20, func(results map[string]map[string]any) {
				results["pcb.components.list"] = map[string]any{"components": []any{component}}
			})
			ants, _, layers, err := fetchAntennaContext(cfg, "w1", nil)
			if err != nil || len(ants) != 1 || ants[0].Incomplete != "" || ants[0].RequiredRect == nil || layers != 2 {
				t.Fatalf("ants=%+v layers=%d err=%v", ants, layers, err)
			}
			bb, _ := antennaCommandBBox(component)
			var expected [4]float64
			if chip {
				x0, y0, x1, y1, _ := chipAntennaKeepoutRect(bb.MinX, bb.MinY, bb.MaxX, bb.MaxY, 120)
				expected = [4]float64{x0, y0, x1, y1}
				if len(ants[0].RequiredRules) != 2 || ants[0].RequiredRules[0] != 6 {
					t.Fatalf("chip rules=%v", ants[0].RequiredRules)
				}
			}
			if chip && *ants[0].RequiredRect != expected {
				t.Fatalf("rect=%v want=%v", *ants[0].RequiredRect, expected)
			}
			if !chip && ((*ants[0].RequiredRect)[0] != -20 || (*ants[0].RequiredRect)[3] != 920 || (*ants[0].RequiredRect)[1] <= 400) {
				t.Fatalf("module does not use pad-free end/margin: %v", *ants[0].RequiredRect)
			}
		})
	}
}

func TestPcbCheckAntennaMissingEvidenceIsIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any, map[string]map[string]any)
	}{
		{"bbox missing", func(c map[string]any, _ map[string]map[string]any) { delete(c, "bbox") }},
		{"pad inventory missing", func(c map[string]any, _ map[string]map[string]any) { delete(c, "pads") }},
		{"pad inventory empty", func(c map[string]any, _ map[string]map[string]any) { c["pads"] = []any{} }},
		{"pad capture unavailable", func(c map[string]any, _ map[string]map[string]any) {
			c["availability"] = map[string]any{"pads": "unavailable"}
		}},
		{"pad pose missing", func(c map[string]any, _ map[string]map[string]any) {
			delete(c["pads"].([]any)[0].(map[string]any), "x")
		}},
		{"layer count missing", func(_ map[string]any, r map[string]map[string]any) { r["pcb.layers.list"] = map[string]any{} }},
		{"region inventory missing", func(_ map[string]any, r map[string]map[string]any) { r["pcb.region.list"] = map[string]any{} }},
		{"region geometry missing", func(_ map[string]any, r map[string]map[string]any) {
			r["pcb.region.list"] = map[string]any{"regions": []any{map[string]any{"primitiveId": "r", "layer": 12, "ruleType": []any{5, 6, 7, 8}}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newClearanceCheckFixture(t, 20, func(results map[string]map[string]any) {
				c := antennaContextTestComponent(false)
				results["pcb.components.list"] = map[string]any{"components": []any{c}}
				results["pcb.line.list"] = map[string]any{"lines": []any{}, "arcs": []any{}, "arcsAvailable": true}
				tc.change(c, results)
			})
			var diagnostics bytes.Buffer
			report, err := gatherPcbCheckReport(cfg, "w1", 3, nil, &diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Incomplete || report.Passed || !strings.Contains(strings.Join(report.Limitations, "; "), "antenna") {
				t.Fatalf("incomplete=%v passed=%v limits=%v", report.Incomplete, report.Passed, report.Limitations)
			}
		})
	}
}
