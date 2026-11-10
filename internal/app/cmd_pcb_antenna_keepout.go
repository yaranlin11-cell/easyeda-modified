package app

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/zhoushoujianwork/easyeda-agent/internal/blocks"
)

type pcbAntennaKeepoutOptions struct {
	DryRun                           bool
	Margin, PadClearance, ChipMargin float64
}

type pcbAntennaKeepoutPlan struct {
	Designator   string         `json:"designator"`
	Device       string         `json:"device"`
	Rect         [4]float64     `json:"rect"`
	Chip         bool           `json:"chipAntenna,omitempty"`
	Created      bool           `json:"created"`
	Attempted    bool           `json:"attempted,omitempty"`
	StateUnknown bool           `json:"stateUnknown,omitempty"`
	PrimitiveID  string         `json:"primitiveId,omitempty"`
	Actual       map[string]any `json:"actual,omitempty"`
	Error        string         `json:"error,omitempty"`
}

func runPcbAntennaKeepout(cfg *appConfig, window string, opts pcbAntennaKeepoutOptions, stdout io.Writer) error {
	if opts.DryRun {
		defer setDispatchDryRun(true)()
	}
	for name, value := range map[string]float64{"margin": opts.Margin, "pad-clearance": opts.PadClearance, "chip-margin": opts.ChipMargin} {
		if !isFinite(value) || value < 0 {
			return fmt.Errorf("%s must be a finite non-negative number (mil)", name)
		}
	}
	window, err := resolveTargetWindow(cfg, window)
	if err != nil {
		return err
	}
	var plan []pcbAntennaKeepoutPlan
	var skipped []map[string]any
	created := 0
	report := func(cause error) error {
		partial := created > 0
		stateUnknown := false
		for _, entry := range plan {
			if entry.PrimitiveID != "" || entry.Actual["partial"] == true {
				partial = true
			}
			stateUnknown = stateUnknown || entry.StateUnknown
		}
		out := map[string]any{"ok": cause == nil, "dryRun": opts.DryRun, "layer": pcbLayerMulti,
			"planned": len(plan), "created": created, "regions": plan, "skipped": skipped}
		if cause != nil {
			out["error"], out["partial"] = cause.Error(), partial
			if stateUnknown {
				out["stateUnknown"] = true
			}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
		return cause
	}
	res, err := requestAction(cfg, "pcb.components.list", window, map[string]any{"includeBBox": true, "includePads": true})
	if err != nil {
		return report(err)
	}
	components, ok := res.Result["components"].([]any)
	if !ok {
		return report(fmt.Errorf("antenna keepout: component inventory unavailable"))
	}
	keepouts, err := blocks.LoadAntennaKeepouts()
	if err != nil {
		return report(fmt.Errorf("antenna keepout depth declarations: %w", err))
	}
	silkDev := map[string]string{}
	silk, err := fetchPcbSilk(cfg, window)
	if err != nil {
		return report(fmt.Errorf("antenna device attributes unavailable: %w", err))
	}
	for _, s := range silk {
		if s.Kind == "attribute" && s.Key == "Device" && s.CompID != "" {
			silkDev[s.CompID] = s.Text
		}
	}
	var antennas []map[string]any
	for _, raw := range components {
		cm, ok := raw.(map[string]any)
		if !ok {
			return report(fmt.Errorf("antenna component inventory contains an unreadable record"))
		}
		device := resolveAntennaDevice(silkDev[asString(cm["primitiveId"])], cm)
		if isAntennaDevice(device, asString(cm["designator"])) {
			antennas = append(antennas, cm)
		}
	}
	if len(antennas) == 0 {
		return report(nil)
	}
	lres, err := requestAction(cfg, "pcb.layers.list", window, nil)
	if err != nil {
		return report(fmt.Errorf("antenna copper layers unavailable: %w", err))
	}
	layerCount, ok := asFloatOK(lres.Result["copperLayerCount"])
	if !ok || !isFinite(layerCount) || layerCount < 2 || layerCount > 32 || layerCount != float64(int(layerCount)) || int(layerCount)%2 != 0 {
		return report(fmt.Errorf("antenna copper layer count unavailable or invalid"))
	}
	rres, err := requestAction(cfg, "pcb.region.list", window, nil)
	if err != nil {
		return report(fmt.Errorf("antenna region inventory unavailable: %w", err))
	}
	if _, ok := rres.Result["regions"].([]any); !ok {
		return report(fmt.Errorf("antenna region inventory unavailable (explicit array required)"))
	}
	existing := parseAntennaKeepRegions(rres.Result)
	// Copper cannot exist past the board edge, so coverage is judged only over
	// the on-board part of the planned rect (live 2026-10-13: the 20 mil outward
	// margin crossed the edge and a board-wide keep-out never counted as cover).
	// Unreadable outline → judge the full rect (conservative).
	board := fetchAntennaBoardBBox(cfg, window)
	for _, cm := range antennas {
		device := resolveAntennaDevice(silkDev[asString(cm["primitiveId"])], cm)
		desig := asString(cm["designator"])
		for _, key := range []string{"bbox", "pads"} {
			if issue := antennaCommandCaptureIssue(cm, key); issue != "" {
				return report(fmt.Errorf("antenna %s: %s", desig, issue))
			}
		}
		bb, err := antennaCommandBBox(cm)
		if err != nil {
			skipped = append(skipped, map[string]any{"designator": desig, "reason": err.Error()})
			return report(fmt.Errorf("antenna %s: %w", desig, err))
		}
		padRecords, ok := cm["pads"].([]any)
		if !ok || len(padRecords) == 0 {
			return report(fmt.Errorf("antenna %s: pad inventory unavailable or empty", desig))
		}
		var pads [][2]float64
		for _, pi := range padRecords {
			pm, ok := pi.(map[string]any)
			if !ok {
				return report(fmt.Errorf("antenna %s: unreadable pad record", desig))
			}
			x, xok := asFloatOK(pm["x"])
			y, yok := asFloatOK(pm["y"])
			if !xok || !yok || !isFinite(x) || !isFinite(y) {
				return report(fmt.Errorf("antenna %s: pad coordinates unavailable", desig))
			}
			pads = append(pads, [2]float64{x, y})
		}
		var x0, y0, x1, y1 float64
		chip := isChipAntennaPart(device, bb.MaxX-bb.MinX, bb.MaxY-bb.MinY)
		if chip {
			x0, y0, x1, y1, ok = chipAntennaKeepoutRect(bb.MinX, bb.MinY, bb.MaxX, bb.MaxY, opts.ChipMargin)
		} else {
			x0, y0, x1, y1, ok = antennaKeepoutRect(bb.MinX, bb.MinY, bb.MaxX, bb.MaxY, pads,
				antennaKeepoutFrac(keepouts, device), opts.Margin, opts.PadClearance)
		}
		if !ok {
			skipped = append(skipped, map[string]any{"designator": desig, "reason": "no readable pad-free antenna strip"})
			return report(fmt.Errorf("antenna %s: cannot derive a pad-free strip (bbox/pads or explicit source parameters required)", desig))
		}
		rect := [4]float64{x0, y0, x1, y1}
		if !chip {
			rect = antennaEdgeSpan(rect, bb, board)
		}
		rules := antennaCommandRules(chip)
		if len(antennaCoverageMissing(antennaOnBoardRect(rect, board), rules[:len(rules)-1], existing, int(layerCount))) == 0 {
			skipped = append(skipped, map[string]any{"designator": desig, "reason": "already covered by complete polygon/rule/layer evidence"})
			continue
		}
		for _, region := range existing {
			if region.Layer == pcbLayerMulti && antennaCommandHasRules(region.RuleTypes, rules) && !region.GeometryAvailable {
				return report(fmt.Errorf("antenna %s: existing full-rule region %s geometry unavailable; inspect before creating a possible duplicate", desig, region.PrimitiveID))
			}
		}
		plan = append(plan, pcbAntennaKeepoutPlan{Designator: desig, Device: device, Rect: rect, Chip: chip})
	}
	for i := range plan {
		if opts.DryRun {
			continue
		}
		entry := &plan[i]
		entry.Attempted = true
		rules := antennaCommandRules(entry.Chip)
		res, createErr := requestAction(cfg, "pcb.region.create", window, map[string]any{
			"points": rectCorners(entry.Rect[0], entry.Rect[1], entry.Rect[2], entry.Rect[3]),
			"layer":  pcbLayerMulti, "ruleType": rules,
		})
		if res != nil {
			entry.Actual = res.Result
			entry.PrimitiveID = strings.TrimSpace(asString(res.Result["primitiveId"]))
		}
		// Host errors may follow a landed write. Only an explicit pre-write refusal
		// can prove zero writes when the response carries no accountable PID.
		knownNoWrite := res != nil && !res.OK && antennaKnownPrewriteRefusal(res.errorCode)
		entry.StateUnknown = entry.PrimitiveID == "" && !knownNoWrite
		if createErr == nil {
			createErr = antennaCreatedEvidence(entry, rules)
		}
		if createErr != nil {
			entry.Error = createErr.Error()
			return report(fmt.Errorf("antenna %s region creation failed: %w; preserve actual IDs and inspect fresh state before retrying", entry.Designator, createErr))
		}
		entry.Created = true
		created++
	}
	return report(nil)
}

func antennaCreatedEvidence(entry *pcbAntennaKeepoutPlan, rules []int) error {
	if entry.Actual["verified"] != true || entry.Actual["partial"] == true || entry.PrimitiveID == "" {
		return fmt.Errorf("region write not fully verified (primitiveId=%q, verified=%v, partial=%v)", entry.PrimitiveID, entry.Actual["verified"], entry.Actual["partial"])
	}
	regions := parseAntennaKeepRegions(map[string]any{"regions": []any{entry.Actual}})
	if len(regions) != 1 || regions[0].Layer != pcbLayerMulti || !antennaRegionCovers(entry.Rect, rules, regions[0]) {
		return fmt.Errorf("created region %s lacks complete fresh polygon/rule/layer evidence", entry.PrimitiveID)
	}
	if len(regions[0].RuleTypes) != len(rules) {
		return fmt.Errorf("created region %s rules differ from the requested rules", entry.PrimitiveID)
	}
	r := entry.Rect
	expected := pointsPolygonSource([][2]float64{{r[0], r[1]}, {r[2], r[1]}, {r[2], r[3]}, {r[0], r[3]}})
	equal, err := polygonSourcesEquivalent(entry.Actual["source"], expected)
	if err != nil || !equal {
		return fmt.Errorf("created region %s polygon differs from the requested polygon", entry.PrimitiveID)
	}
	return nil
}

func antennaKnownPrewriteRefusal(code string) bool {
	// pcb.region.create validates required/optional payload fields before calling
	// the SDK create method. These dispatcher/refusal codes also precede writes.
	switch code {
	case "PRECONDITION_REFUSED", "MISSING_PAYLOAD_FIELD", "UNKNOWN_ACTION", "EDA_API_UNAVAILABLE":
		return true
	}
	return false
}

func antennaCommandRules(chip bool) []int {
	if chip {
		return []int{6, 7, 8}
	}
	return []int{5, 6, 7, 8}
}

func antennaCommandHasRules(actual, required []int) bool {
	for _, expected := range required {
		found := false
		for _, rule := range actual {
			if rule == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func antennaCommandBBox(component map[string]any) (layoutBBox, error) {
	bb, ok := component["bbox"].(map[string]any)
	if !ok {
		return layoutBBox{}, fmt.Errorf("rendered bbox unavailable")
	}
	var v [4]float64
	for i, key := range []string{"minX", "minY", "maxX", "maxY"} {
		value, ok := asFloatOK(bb[key])
		if !ok || !isFinite(value) {
			return layoutBBox{}, fmt.Errorf("rendered bbox %s unavailable", key)
		}
		v[i] = value
	}
	if v[2] <= v[0] || v[3] <= v[1] {
		return layoutBBox{}, fmt.Errorf("rendered bbox is degenerate")
	}
	return layoutBBox{MinX: v[0], MinY: v[1], MaxX: v[2], MaxY: v[3]}, nil
}

func antennaCommandCaptureIssue(component map[string]any, key string) string {
	if issue := pcbCaptureErrors(component["captureErrors"])[key]; issue != nil {
		return fmt.Sprintf("%s capture failed: %v", key, issue)
	}
	if state, exists := pcbStringMap(component["availability"])[key]; exists && state != "available" {
		return fmt.Sprintf("%s capture is %s", key, state)
	}
	if key == "pads" && len(mnavSlice(component, "padFailures")) > 0 {
		return "pad capture is incomplete"
	}
	return ""
}

// antennaOnBoardRect clips a planned keep-out rect to the board outline bbox
// for coverage judgement. No outline, or no overlap, keeps the full rect.
func antennaOnBoardRect(rect [4]float64, board *layoutBBox) [4]float64 {
	if board == nil {
		return rect
	}
	clipped := [4]float64{math.Max(rect[0], board.MinX), math.Max(rect[1], board.MinY), math.Min(rect[2], board.MaxX), math.Min(rect[3], board.MaxY)}
	if clipped[0] >= clipped[2] || clipped[1] >= clipped[3] {
		return rect
	}
	return clipped
}
