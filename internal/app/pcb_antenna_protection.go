package app

import (
	"math"
	"strings"
)

// A small/partial module bbox cannot turn a WROOM into a feed-permitting chip.
func isChipAntennaPart(device string, w, h float64) bool {
	if !antennaFinite(w) || !antennaFinite(h) || !isChipAntennaSize(w, h) {
		return false
	}
	d := strings.ToUpper(strings.TrimSpace(device))
	for _, module := range []string{"WROOM", "WROVER", "ESP32-C3-MINI", "ESP8266"} {
		if strings.Contains(d, module) {
			return false
		}
	}
	return strings.Contains(d, "2450AT") || strings.Contains(d, "ANT-SMD") ||
		strings.Contains(d, "ANTENNA") || strings.HasPrefix(d, "ANT")
}

// antennaLinearPolygon accepts only a single, explicitly closed linear contour.
// An ARC approximation or a bbox cannot prove an antenna's complete keep-out.
func antennaLinearPolygon(source any) ([][2]float64, bool) {
	flat, ok := source.([]any)
	if !ok || len(flat) == 0 {
		return nil, false
	}
	if _, nested := flat[0].([]any); nested {
		if len(flat) != 1 {
			return nil, false
		}
		return antennaLinearPolygon(flat[0])
	}
	for _, token := range flat {
		if command, ok := token.(string); ok && command != "L" {
			return nil, false
		}
	}
	if len(flat) < 9 {
		return nil, false
	}
	firstX, xok := asFloatOK(flat[0])
	firstY, yok := asFloatOK(flat[1])
	lastX, lxok := asFloatOK(flat[len(flat)-2])
	lastY, lyok := asFloatOK(flat[len(flat)-1])
	if !xok || !yok || !lxok || !lyok || math.Hypot(firstX-lastX, firstY-lastY) > netPathGeomEps {
		return nil, false
	}
	contours, err := polygonSourceContours(flat)
	if err != nil || len(contours) != 1 {
		return nil, false
	}
	points := contours[0]
	for i, a := range points {
		b := points[(i+1)%len(points)]
		if !antennaFinite(a[0]) || !antennaFinite(a[1]) || math.Hypot(a[0]-b[0], a[1]-b[1]) <= netPathGeomEps {
			return nil, false
		}
	}
	if _, err := buildCompoundPolygonTopology(contours); err != nil {
		return nil, false
	}
	return points, true
}

func antennaFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// parseAntennaKeepRegions accepts a region-list result or a region-create result.
// Missing availability on create is allowed only when the source itself proves
// readable geometry; an explicit unavailable marker always wins.
func parseAntennaKeepRegions(result map[string]any) []pcbKeepRegion {
	raw, _ := result["regions"].([]any)
	if _, single := result["primitiveId"]; single && raw == nil {
		raw = []any{result}
	}
	var regions []pcbKeepRegion
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		r := pcbKeepRegion{PrimitiveID: asString(m["primitiveId"]), Name: asString(m["regionName"]), Source: m["source"]}
		layerValid := false
		if layer, ok := asFloatOK(m["layer"]); ok && antennaFinite(layer) && layer == math.Trunc(layer) &&
			(layer == 1 || layer == 2 || layer == 12 || layer >= 15 && layer <= 44) {
			r.Layer = int(layer)
			layerValid = true
		}
		rawRules, rulesValid := m["ruleType"].([]any)
		rulesValid = rulesValid && len(rawRules) > 0
		for _, rawRule := range rawRules {
			rule, valid := asFloatOK(rawRule)
			if !valid || !antennaFinite(rule) || rule != math.Trunc(rule) ||
				!(rule == 2 || rule >= 5 && rule <= 9) {
				rulesValid = false
				break
			}
			r.RuleTypes = append(r.RuleTypes, int(rule))
		}
		points, valid := antennaLinearPolygon(r.Source)
		r.GeometryAvailable = valid && layerValid && rulesValid && m["geometryAvailable"] != false
		if valid {
			box := layoutBBox{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
			for _, p := range points {
				box.MinX, box.MinY = math.Min(box.MinX, p[0]), math.Min(box.MinY, p[1])
				box.MaxX, box.MaxY = math.Max(box.MaxX, p[0]), math.Max(box.MaxY, p[1])
			}
			r.BBox = &box
		}
		regions = append(regions, r)
	}
	return regions
}

// antennaRegionCovers requires all requested rules in one region and complete
// polygon containment. Several partial regions/rules are not a coverage proof.
func antennaRegionCovers(rect [4]float64, requiredRules []int, region pcbKeepRegion) bool {
	if strings.TrimSpace(region.PrimitiveID) == "" || !region.GeometryAvailable || len(requiredRules) == 0 || !antennaValidRect(rect) {
		return false
	}
	rules := make(map[int]bool, len(region.RuleTypes))
	for _, rule := range region.RuleTypes {
		rules[rule] = true
	}
	for _, rule := range requiredRules {
		if !rules[rule] {
			return false
		}
	}
	points, ok := antennaLinearPolygon(region.Source)
	if !ok {
		return false
	}
	for _, corner := range [][2]float64{{rect[0], rect[1]}, {rect[2], rect[1]}, {rect[2], rect[3]}, {rect[0], rect[3]}} {
		if !pointInOrOnSimplePolygon(points, corner) {
			return false
		}
	}
	// Corner tests alone miss a concave notch. No polygon boundary may enter the
	// rectangle's open interior, even when both endpoints lie outside it.
	for i, a := range points {
		if antennaSegmentEntersRect(a, points[(i+1)%len(points)], rect) {
			return false
		}
	}
	return true
}

func antennaValidRect(rect [4]float64) bool {
	for _, v := range rect {
		if !antennaFinite(v) {
			return false
		}
	}
	return rect[2] > rect[0] && rect[3] > rect[1]
}

func antennaSegmentEntersRect(a, b [2]float64, rect [4]float64) bool {
	lo, hi := 0.0, 1.0
	for axis := 0; axis < 2; axis++ {
		min, max := rect[axis]+netPathGeomEps, rect[axis+2]-netPathGeomEps
		if min >= max {
			return true // geometry thinner than the proof tolerance is unavailable
		}
		d := b[axis] - a[axis]
		if d == 0 {
			if a[axis] <= min || a[axis] >= max {
				return false
			}
			continue
		}
		u, v := (min-a[axis])/d, (max-a[axis])/d
		if u > v {
			u, v = v, u
		}
		lo, hi = math.Max(lo, u), math.Min(hi, v)
		if lo >= hi {
			return false
		}
	}
	return lo < hi
}

// With only a layer count, MULTI + rule 8 is the conservative multilayer proof:
// TOP/BOTTOM + no-inner-electrical does not cover inner signal layers.
func antennaCoverageMissing(rect [4]float64, requiredRules []int, regions []pcbKeepRegion, copperLayers int) []string {
	if copperLayers < 2 || copperLayers > 32 || copperLayers%2 != 0 {
		return []string{"layers-unavailable"}
	}
	if !antennaValidRect(rect) || len(requiredRules) == 0 {
		return []string{"antenna-requirements-unavailable"}
	}
	if copperLayers > 2 {
		rules := append(append([]int(nil), requiredRules...), 8)
		for _, region := range regions {
			if region.Layer == pcbLayerMulti && antennaRegionCovers(rect, rules, region) {
				return nil
			}
		}
		return []string{"all copper layers (MULTI with complete rules and no-inner-electrical)"}
	}
	top, bottom := false, false
	for _, region := range regions {
		if !antennaRegionCovers(rect, requiredRules, region) {
			continue
		}
		top = top || region.Layer == pcbSideTop || region.Layer == pcbLayerMulti
		bottom = bottom || region.Layer == pcbSideBottom || region.Layer == pcbLayerMulti
	}
	var missing []string
	if !top {
		missing = append(missing, "top (L1)")
	}
	if !bottom {
		missing = append(missing, "bottom (L2)")
	}
	return missing
}
