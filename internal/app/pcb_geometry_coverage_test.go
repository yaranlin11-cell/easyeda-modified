package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit native shape tuples are the evidence for these copper fixtures.
// The two components are on opposite faces, so body overlap cannot mask the
// intended TOP-vs-MULTI copper corner-contact regression.
func nativeCornerSnapshot(shape []any) *boardSnapshot {
	pad := func(id, net string, layer int, x, y float64) boardPad {
		return boardPad{ID: id, Number: "1", Net: net, Layer: layer, X: x, Y: y, W: 20, H: 20,
			Shape: shape, Availability: map[string]string{"shape": "available", "specialPad": "available"}}
	}
	return &boardSnapshot{Components: []boardComp{
		{ID: "a", Designator: "A1", Layer: pcbSideTop, BBox: bb(-10, -10, 10, 10), Pads: []boardPad{pad("pa", "A", pcbSideTop, 0, 0)}, Availability: map[string]string{"pads": "available", "bbox": "available"}},
		{ID: "b", Designator: "B1", Layer: pcbSideBottom, BBox: bb(8, 8, 28, 28), Pads: []boardPad{pad("pb", "B", pcbLayerMulti, 18, 18)}, Availability: map[string]string{"pads": "available", "bbox": "available"}},
	}}
}

func TestLayoutNativeRectCornerContactAndCircleControl(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shape  []any
		shorts int
	}{
		{"square", []any{"RECT", 20.0, 20.0, 0.0}, 1},
		{"circle", []any{"ELLIPSE", 20.0, 20.0}, 0},
		{"roundrect", []any{"RECT", 20.0, 20.0, 10.0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := nativeCornerSnapshot(tc.shape)
			comps, pads := snap.toLayoutComps()
			rep := analyzePcbLayout(comps, pads, nil, 6)
			if rep.Incomplete || len(rep.Shorts) != tc.shorts || len(rep.Overlaps) != 0 {
				t.Fatalf("native contact=%+v", rep)
			}
			if tc.shorts == 1 {
				short := rep.Shorts[0]
				if short.OvX != 2 || short.OvY != 2 || short.Layer != "multi" {
					t.Fatalf("corner overlap=%+v", short)
				}
			}
			snap.Components[1].Pads[0].Layer = pcbSideBottom
			comps, pads = snap.toLayoutComps()
			if separated := analyzePcbLayout(comps, pads, nil, 6); len(separated.Shorts) != 0 {
				t.Fatalf("opposite SMD sides=%+v", separated.Shorts)
			}
		})
	}
}

func TestLayoutExactGeometryUsesRotationAndOval(t *testing.T) {
	a := pcbLPad{Designator: "U1", Number: "1", Net: "A", Layer: 1, X: 0, Y: 0, W: 6, H: 40, Rotation: 45, Shape: []any{"RECT", 40.0, 6.0, 0.0}}
	b := pcbLPad{Designator: "U2", Number: "1", Net: "B", Layer: 1, X: 12, Y: 12, W: 10, H: 10, Shape: []any{"RECT", 10.0, 10.0, 0.0}}
	if shorts := padShorts([]pcbLPad{a}, []pcbLPad{b}); len(shorts) != 1 {
		t.Fatalf("rotated native rectangle was clipped by W/H: %+v", shorts)
	}
	a.Rotation = 0
	if shorts := padShorts([]pcbLPad{a}, []pcbLPad{b}); len(shorts) != 0 {
		t.Fatalf("unrotated control: %+v", shorts)
	}
	a.W, a.H, a.Shape = 20, 10, []any{"OVAL", 20.0, 10.0}
	b.X, b.Y, b.W, b.H, b.Shape = 18, 8, 20, 10, []any{"OVAL", 20.0, 10.0}
	if shorts := padShorts([]pcbLPad{a}, []pcbLPad{b}); len(shorts) != 0 {
		t.Fatalf("capsule corner envelope is not copper: %+v", shorts)
	}
}

func TestLayoutMissingBBoxStillReportsKnownCopperShort(t *testing.T) {
	snap := nativeCornerSnapshot([]any{"RECT", 20.0, 20.0, 0.0})
	snap.Components[0].BBox = nil
	comps, pads := snap.toLayoutComps()
	rep := analyzePcbLayout(comps, pads, nil, 6)
	if len(rep.Shorts) != 1 || !rep.Incomplete || len(rep.Coverage.MissingBBoxComponents) != 1 {
		t.Fatalf("missing bbox hid short/coverage: %+v", rep)
	}
}

func TestBoardGeometryCoverageDistinguishesKnownEmptyUnknownAndUnsupported(t *testing.T) {
	component := map[string]any{"primitiveId": "u1", "designator": "U1", "bbox": map[string]any{"minX": 0.0, "minY": 0.0, "maxX": 10.0, "maxY": 10.0}, "availability": map[string]any{"pads": "available", "bbox": "available"}, "pads": []any{}}
	if coverage := pcbReadbackCoverage(componentsResult(component), true); !coverage.Complete || coverage.TotalPads != 0 {
		t.Fatalf("known empty: %+v", coverage)
	}
	delete(component, "pads")
	if coverage := pcbReadbackCoverage(componentsResult(component), true); coverage.Complete || coverage.PadsInventoryKnown {
		t.Fatalf("absent pads: %+v", coverage)
	}
	component["pads"] = nil
	if coverage := pcbReadbackCoverage(componentsResult(component), true); coverage.Complete || coverage.PadsInventoryKnown {
		t.Fatalf("null pads: %+v", coverage)
	}
	for _, tc := range []struct {
		name   string
		shape  any
		status string
	}{
		{"legacy", nil, "unknown"},
		{"polygon", []any{"POLYGON", []any{0.0, 0.0, "L", 1.0, 0.0, 0.0, 1.0}}, "unsupported"},
		{"ellipse", []any{"ELLIPSE", 20.0, 10.0}, "unsupported"},
		{"ngon", []any{"NGON", 20.0, 6.0}, "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			component["pads"] = []any{map[string]any{"primitiveId": "p1", "padNumber": "1", "layer": 1.0, "x": 2.0, "y": 3.0, "rotation": 0.0, "width": 20.0, "height": 10.0, "shape": tc.shape}}
			coverage := pcbReadbackCoverage(componentsResult(component), true)
			if coverage.Complete || len(coverage.UnverifiedPads) != 1 || coverage.UnverifiedPads[0].Status != tc.status || coverage.UnverifiedPads[0].PrimitiveID != "p1" || coverage.UnverifiedPads[0].ComponentPrimitiveID != "u1" {
				t.Fatalf("unverified source=%+v", coverage)
			}
			if tc.name == "legacy" && coverage.LegacyGeometryPads != 1 {
				t.Fatalf("legacy extent claimed exact: %+v", coverage)
			}
		})
	}
}

func TestBoardGeometryCaptureFailuresRemainUnknownAndPreserveDetails(t *testing.T) {
	for _, key := range []string{"shape", "specialPad"} {
		t.Run(key, func(t *testing.T) {
			pad := map[string]any{"primitiveId": "p1", "padNumber": "7", "layer": 1.0, "x": 2.0, "y": 3.0, "rotation": 0.0, "shape": []any{"RECT", 20.0, 10.0, 0.0},
				"availability": map[string]any{key: "unavailable"}, "captureErrors": map[string]any{key: map[string]any{"code": "getter-failed", "message": "source getter threw"}}}
			result := componentsResult(map[string]any{"primitiveId": "u1", "designator": "U1", "pads": []any{pad}, "availability": map[string]any{"pads": "available"}})
			coverage := pcbReadbackCoverage(result, false)
			if coverage.Complete || coverage.UnknownGeometryPads != 1 || !strings.Contains(coverage.UnverifiedPads[0].Reason, "getter-failed") || !strings.Contains(coverage.UnverifiedPads[0].Reason, "source getter threw") {
				t.Fatalf("capture failure vanished: %+v", coverage)
			}
			result["components"].([]any)[0].(map[string]any)["padFailures"] = []any{map[string]any{"index": 1, "error": map[string]any{"code": "bad-pad"}}}
			if c := pcbReadbackCoverage(result, false); c.PadsInventoryKnown {
				t.Fatalf("partial pad inventory certified: %+v", c)
			}
		})
	}
}

func TestBoardSnapshotNativeIdentityGeometryAndKnownEmptyRoundTrip(t *testing.T) {
	source := map[string]any{"primitiveId": "u1", "uniqueId": "native-instance", "designator": "U1", "footprint": map[string]any{"uuid": "footprint-uuid", "libraryUuid": "library-uuid", "name": "QFN"}, "pads": []any{}, "availability": map[string]any{"pads": "available"}}
	snap := &boardSnapshot{Components: parseBoardComponents(componentsResult(source))}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBoardSnapshotFile(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := loaded.Components[0]
	if c.UniqueID != "native-instance" || c.Pads == nil || canonicalJSON(c.Footprint) != canonicalJSON(source["footprint"]) || !loaded.geometryCoverage(false).Complete {
		t.Fatalf("native snapshot projection lost data: %+v", c)
	}
	snap = nativeCornerSnapshot([]any{"RECT", 20.0, 10.0, 2.0})
	snap.Components[0].Pads[0].Rotation = 37
	native := snap.toCheckPads()[0]
	if !native.ShapeOK || native.Shape != "RECT" || native.Rotation != 37 || native.ShapeRound != 2 || native.ID != "pa" {
		t.Fatalf("check projection lost source geometry: %+v", native)
	}
	noSpecial := false
	snap.Components[0].Pads[0].SpecialPadPresent = &noSpecial
	raw, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = loadBoardSnapshotFile(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if present := loaded.Components[0].Pads[0].SpecialPadPresent; present == nil || *present {
		t.Fatalf("explicit absent optional special geometry lost: %+v", loaded.Components[0].Pads[0])
	}
}

func TestLayoutScoreIncompleteRejectsExplicitThreshold(t *testing.T) {
	snap := nativeCornerSnapshot([]any{"RECT", 20.0, 20.0, 0.0})
	snap.Components = snap.Components[:1]
	snap.Components[0].Pads[0].Shape = nil
	path := filepath.Join(t.TempDir(), "board.json")
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		threshold bool
	}{{"diagnostic", false}, {"explicit-threshold", true}} {
		t.Run(tc.name, func(t *testing.T) {
			window := ""
			var stdout, stderr bytes.Buffer
			cmd := newPcbLayoutScoreCmd(&appConfig{}, &window, &stdout, &stderr)
			args := []string{"--from", path, "--only", "tidy", "--json"}
			if tc.threshold {
				args = append(args, "--min-score", "99")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if tc.threshold && (err == nil || !strings.Contains(err.Error(), "incomplete")) || !tc.threshold && err != nil {
				t.Fatalf("threshold error=%v", err)
			}
			var response struct {
				Report layoutScoreReport `json:"report"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Report.Verdict != "incomplete" || !response.Report.Incomplete || response.Report.Overall != 100 || response.Report.OK {
				t.Fatalf("unverified perfect score accepted: %+v", response.Report)
			}
		})
	}
}

func TestLayoutUnknownShellOverlapIsNotCertifiedUnderShell(t *testing.T) {
	snap := nativeCornerSnapshot([]any{"RECT", 20.0, 20.0, 0.0})
	snap.Components[0].Designator = "J1"
	snap.Components[1].Layer = pcbSideTop
	snap.Components[1].Pads[0].Shape = nil
	comps, pads := snap.toLayoutComps()
	rep := analyzePcbLayout(comps, pads, nil, 6)
	if !rep.Incomplete || len(rep.UnderShellPairs) != 0 || len(rep.UnverifiedOverlaps) != 1 || evalLayoutGate(rep, pcbLayoutGateOpts{minScore: 99, maxCrossings: 8}).Pass {
		t.Fatalf("unknown shell space certified free: %+v", rep)
	}
}

func TestBoardMissingPadPoseIsPreservedButCannotProduceOriginFindings(t *testing.T) {
	pad := map[string]any{"primitiveId": "p1", "padNumber": "7", "net": "N1", "layer": 1.0,
		"shape": []any{"RECT", 20.0, 10.0, 0.0}, "width": 20.0, "height": 10.0, "rotation": 0.0}
	result := componentsResult(map[string]any{"primitiveId": "u1", "designator": "U1", "pads": []any{pad}, "availability": map[string]any{"pads": "available"}})
	snap := &boardSnapshot{Components: parseBoardComponents(result)}
	coverage := snap.geometryCoverage(false)
	if len(snap.Components[0].Pads) != 1 || coverage.Complete || coverage.UnknownGeometryPads != 1 || len(snap.toCheckPads()) != 0 {
		t.Fatalf("unknown pose manufactured an origin obstacle: coverage=%+v pads=%+v", coverage, snap.toCheckPads())
	}
	snap.Components[0].BBox = bb(40, 40, 60, 60)
	comps, layoutPads := snap.toLayoutComps()
	layout := analyzePcbLayout(comps, layoutPads, bb(30, 30, 70, 70), 6)
	if !layout.Incomplete || len(layout.OutsideOutline) != 0 {
		t.Fatalf("missing pose manufactured off-board pad: %+v", layout)
	}
	pad["x"], pad["y"] = 3.0, 4.0
	pad["availability"] = map[string]any{"shape": "available", "specialPad": "unavailable"}
	pad["captureErrors"] = map[string]any{"specialPad": map[string]any{"code": "getter-failed", "message": "injected"}}
	snap.Components = parseBoardComponents(result)
	observed := snap.toCheckPads()
	if len(observed) != 1 || observed[0].ShapeOK || observed[0].W != 0 || observed[0].H != 0 {
		t.Fatalf("explicit failure became legacy shape: %+v", observed)
	}
}

func TestLayoutIncompleteCannotCertifyOKInPureOrLiveReport(t *testing.T) {
	// Opposite-face components avoid accidental mechanical errors, so a 100
	// diagnostic score cannot mask the missing source geometry in this fixture.
	for _, tc := range []struct {
		name     string
		change   func(*boardSnapshot)
		complete bool
	}{
		{"known native", func(*boardSnapshot) {}, true},
		{"legacy extent-only", func(s *boardSnapshot) { s.Components[0].Pads[0].Shape = nil }, false},
		{"unsupported source", func(s *boardSnapshot) {
			s.Components[0].Pads[0].Shape = []any{"POLYGON", []any{0.0, 0.0, "L", 1.0, 0.0, 0.0, 1.0}}
		}, false},
		{"failed pad inventory", func(s *boardSnapshot) { s.Components[0].Availability["pads"] = "unavailable" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := nativeCornerSnapshot([]any{"ELLIPSE", 20.0, 20.0})
			tc.change(snap)
			comps, pads := snap.toLayoutComps()
			rep := analyzePcbLayout(comps, pads, nil, 6)
			if rep.OK != tc.complete || rep.Incomplete == tc.complete || rep.Score != 100 || len(rep.Shorts)+len(rep.Overlaps)+len(rep.OutsideOutline) != 0 {
				t.Fatalf("pure diagnosis certified missing geometry: %+v", rep)
			}
			raw, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip boardSnapshot
			if err := json.Unmarshal(raw, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if coverage := roundTrip.geometryCoverage(true); coverage.Complete != tc.complete {
				t.Fatalf("snapshot JSON roundtrip changed evidence status: %+v", coverage)
			}
			if first := roundTrip.Components[0].Pads[0]; first.ID != "pa" || first.X != 0 || first.Y != 0 || first.Rotation != 0 || roundTrip.Components[0].ID != "a" {
				t.Fatalf("known zero pose or native identity lost: %+v", first)
			}
			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			assertLiveLayoutCoverage(t, result, tc.complete)
		})
	}
	// This record disappears from the parsed view; only raw readback coverage
	// can retain the defect. The runner must finalize again after that override.
	t.Run("malformed raw inventory", func(t *testing.T) { assertLiveLayoutCoverage(t, map[string]any{"components": []any{nil}}, false) })
}

func TestBoardPoseMissingRemainsUnknownAfterNormalizedJSONRoundTrip(t *testing.T) {
	result := componentsResult(map[string]any{"primitiveId": "u1", "uniqueId": "native-u1", "designator": "U1",
		"pads": []any{map[string]any{"primitiveId": "p1", "padNumber": "1", "layer": 1.0, "shape": []any{"RECT", 20.0, 20.0, 0.0}, "specialPadPresent": false,
			"availability": map[string]any{"shape": "available", "specialPad": "available"}}}, "availability": map[string]any{"pads": "available"}})
	snap := boardSnapshot{Components: parseBoardComponents(result)}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var restored boardSnapshot
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	p := restored.Components[0].Pads[0]
	if restored.Components[0].UniqueID != "native-u1" || p.ID != "p1" || p.SpecialPadPresent == nil || *p.SpecialPadPresent || p.GeometryStatus != "unknown" || !strings.Contains(p.GeometryReason, "pose") {
		t.Fatalf("unknown source or optional false presence lost: %+v", p)
	}
	if coverage := restored.geometryCoverage(false); coverage.Complete || coverage.UnknownGeometryPads != 1 || len(restored.toCheckPads()) != 0 {
		t.Fatalf("serialized defaults became measured pose: %+v", coverage)
	}
	var reparse map[string]any
	if err := json.Unmarshal(raw, &reparse); err != nil {
		t.Fatal(err)
	}
	if coverage := pcbReadbackCoverage(reparse, false); coverage.Complete || coverage.UnknownGeometryPads != 1 {
		t.Fatalf("raw JSON reparse certified default zero pose: %+v", coverage)
	}
}

func assertLiveLayoutCoverage(t *testing.T, result map[string]any, complete bool) {
	t.Helper()
	cfg, _, closeFn := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
		var response map[string]any
		switch call.Action {
		case "pcb.components.list":
			response = result
		case "pcb.outline.get":
			response = map[string]any{"bbox": map[string]any{"minX": -100.0, "minY": -100.0, "maxX": 100.0, "maxY": 100.0}}
		case "pcb.silk.list":
			response = map[string]any{"texts": []any{}}
		default:
			t.Errorf("unexpected action %s", call.Action)
		}
		raw, err := json.Marshal(map[string]any{"ok": true, "result": response})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	})
	defer closeFn()
	var out, diagnostic bytes.Buffer
	if err := runPcbLayoutLint(cfg, "w1", 6, true, pcbLayoutGateOpts{}, &out, &diagnostic); err != nil {
		t.Fatalf("ordinary incomplete diagnosis was gated: %v %s", err, diagnostic.String())
	}
	var response struct {
		Report pcbLayoutReport `json:"report"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	rep := response.Report
	if rep.OK != complete || rep.Incomplete == complete || rep.Coverage.Complete != complete {
		t.Fatalf("live diagnosis certified missing geometry: %+v", rep)
	}
	if !complete && (rep.Verdict != "incomplete" || !strings.Contains(rep.Summary, "(incomplete)") || !strings.Contains(rep.Summary, "geometry inspection incomplete")) {
		t.Fatalf("runner verdict/summary disagree with raw coverage: %+v", rep)
	}
}
