package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic geometry exercises typed CLI propagation; no live design answers.
func TestPcbAntennaKeepoutEvidence(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		failure    bool
		wantWrites int
	}{
		{"created", false, 1},
		{"refused", true, 1},
		{"parameter-refused", true, 1},
		{"host-failed", true, 1},
		{"partial", true, 1},
		{"missing-verified", true, 1},
		{"missing-pid", true, 1},
		{"wrong-polygon", true, 1},
		{"wrong-rules", true, 1},
		{"wrong-layer", true, 1},
		{"geometry-unavailable", true, 1},
		{"no-response", true, 1},
		{"only-no-pours-overlap", false, 1},
		{"full-rules-small-overlap", false, 1},
		{"complete-existing", false, 0},
		{"unknown-existing", true, 0},
		{"unknown-layers", true, 0},
		{"region-read-unavailable", true, 0},
		{"missing-pads", true, 0},
		{"failed-pad-capture", true, 0},
		{"dry-run", false, 0},
		{"chip-feed-exception", false, 1},
		{"no-antenna", false, 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			writes := 0
			var lastPayload map[string]any
			component := map[string]any{
				"primitiveId": "synthetic-module", "designator": "U_SYN", "manufacturerId": "ESP32-S3-WROOM-1",
				"bbox": map[string]any{"minX": -500, "minY": -200, "maxX": 500, "maxY": 1000},
				"pads": []any{map[string]any{"x": -450, "y": 0}, map[string]any{"x": 450, "y": 300}, map[string]any{"x": -450, "y": 600}},
			}
			if tc.mode == "missing-pads" {
				delete(component, "pads")
			}
			if tc.mode == "failed-pad-capture" {
				component["availability"] = map[string]any{"pads": "unavailable", "bbox": "available"}
				component["padFailures"] = []any{map[string]any{"error": "unreadable pad"}}
			}
			if tc.mode == "chip-feed-exception" {
				component["designator"], component["manufacturerId"] = "AE_SYN", "2450AT-SYNTHETIC"
				component["bbox"] = map[string]any{"minX": 0, "minY": 0, "maxX": 120, "maxY": 60}
				component["pads"] = []any{map[string]any{"x": 0, "y": 30}, map[string]any{"x": 120, "y": 30}}
			}
			if tc.mode == "no-antenna" {
				component["designator"], component["manufacturerId"] = "R_SYN", "RESISTOR-SYNTHETIC"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/health" {
					fmt.Fprint(w, `{"service":"easyeda-agent","windows":[{"windowId":"synthetic-window"}]}`)
					return
				}
				var request struct {
					Action  string         `json:"action"`
					Payload map[string]any `json:"payload"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				result := map[string]any{}
				switch request.Action {
				case "pcb.components.list":
					result["components"] = []any{component}
				case "pcb.silk.list":
					result["texts"] = []any{}
				case "pcb.layers.list":
					if tc.mode == "no-antenna" {
						t.Error("no antenna must not require copper-layer evidence")
					}
					if tc.mode != "unknown-layers" {
						result["copperLayerCount"] = 2
					}
				case "pcb.region.list":
					if tc.mode == "region-read-unavailable" {
						fmt.Fprint(w, `{"ok":false,"error":{"code":"EDA_CALL_FAILED","message":"PCB region inventory is unavailable: getAll did not return an array."}}`)
						return
					}
					regions := []any{}
					if strings.Contains(tc.mode, "existing") || strings.Contains(tc.mode, "overlap") {
						points := antennaTestRectPoints(-520, 640, 520, 1020)
						rules := []int{5, 6, 7, 8}
						if strings.Contains(tc.mode, "overlap") {
							points = antennaTestRectPoints(-5, 700, 5, 710)
						}
						if tc.mode == "only-no-pours-overlap" {
							rules = []int{7}
						}
						region := map[string]any{"primitiveId": "existing-region", "layer": 12, "ruleType": rules,
							"source": pointsPolygonSource(points), "geometryAvailable": true,
							"bbox": map[string]any{"minX": -520, "minY": 640, "maxX": 520, "maxY": 1020}}
						if tc.mode == "unknown-existing" {
							region["geometryAvailable"] = false
							delete(region, "source")
						}
						regions = append(regions, region)
					}
					result["regions"] = regions
				case "pcb.region.create":
					writes++
					lastPayload = request.Payload
					if _, exists := request.Payload["name"]; exists {
						t.Error("antenna generator must not name a non-follow-rule region")
					}
					if tc.mode == "refused" || tc.mode == "parameter-refused" || tc.mode == "host-failed" {
						code := map[string]string{"refused": "PRECONDITION_REFUSED", "parameter-refused": "MISSING_PAYLOAD_FIELD", "host-failed": "EDA_CALL_FAILED"}[tc.mode]
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": code, "message": "synthetic write refusal/failure"}})
						return
					}
					if tc.mode == "no-response" {
						return // an empty response gives no evidence whether the write landed
					}
					points, err := pointsFromJSONPayload(request.Payload["points"])
					if err != nil {
						t.Error(err)
						return
					}
					result = map[string]any{"primitiveId": "actual-region", "verified": true, "layer": request.Payload["layer"],
						"ruleType": request.Payload["ruleType"], "source": pointsPolygonSource(points)}
					switch tc.mode {
					case "partial":
						result["verified"], result["partial"] = false, true
						result["differences"] = []string{"lineWidth"}
						result["lineWidth"] = 0.2
					case "missing-verified":
						delete(result, "verified")
					case "missing-pid":
						delete(result, "primitiveId")
					case "wrong-polygon":
						result["source"] = pointsPolygonSource(antennaTestRectPoints(-5, 700, 5, 710))
					case "wrong-rules":
						result["ruleType"] = []int{7}
					case "wrong-layer":
						result["layer"] = 1
					case "geometry-unavailable":
						result["geometryAvailable"] = false
					}
				case "pcb.outline.get":
					// No outline drawn: coverage is judged over the full plan rect,
					// exactly as before the board-edge clip existed.
					result = map[string]any{"segments": float64(0), "centerlineBBox": nil}
				default:
					t.Errorf("unexpected action: %s", request.Action)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
			}))
			defer srv.Close()
			host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
			cfg := &appConfig{host: host, ports: port + "-" + port}
			var out, stderr bytes.Buffer
			cmd := newPcbCmd(cfg, &out, &stderr)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			args := []string{"antenna-keepout", "--window", "synthetic-window"}
			if tc.mode == "dry-run" {
				args = append(args, "--dry-run")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if (err != nil) != tc.failure || writes != tc.wantWrites {
				t.Fatalf("failure=%t writes=%d; want failure=%t writes=%d; err=%v stdout=%s", err != nil, writes, tc.failure, tc.wantWrites, err, out.String())
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["ok"] != !tc.failure {
				t.Fatalf("incorrect envelope: %s", out.String())
			}
			if tc.failure && result["created"] != float64(0) {
				t.Fatalf("unverified write was counted as created: %s", out.String())
			}
			if tc.mode == "partial" && (!strings.Contains(out.String(), "actual-region") || !strings.Contains(out.String(), "0.2") || result["partial"] != true) {
				t.Fatalf("lost accountable partial result: %s", out.String())
			}
			if (tc.mode == "no-response" || tc.mode == "host-failed") && result["stateUnknown"] != true {
				t.Fatalf("lost unknown write outcome: %s", out.String())
			}
			if (tc.mode == "refused" || tc.mode == "parameter-refused") && result["stateUnknown"] == true {
				t.Fatalf("explicit pre-write refusal must remain known no-write: %s", out.String())
			}
			if tc.mode == "chip-feed-exception" {
				for _, rule := range toFloatSlice(lastPayload["ruleType"]) {
					if rule == 5 {
						t.Fatal("chip feed must be allowed into the clearance region")
					}
				}
			}
		})
	}
}

func pointsFromJSONPayload(raw any) ([][2]float64, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("points must be an array")
	}
	var points [][2]float64
	for _, item := range list {
		p, ok := item.([]any)
		if !ok || len(p) != 2 {
			return nil, fmt.Errorf("point must be a pair")
		}
		x, xok := asFloatOK(p[0])
		y, yok := asFloatOK(p[1])
		if !xok || !yok {
			return nil, fmt.Errorf("point must be numeric")
		}
		points = append(points, [2]float64{x, y})
	}
	return points, nil
}

func antennaTestRectPoints(x0, y0, x1, y1 float64) [][2]float64 {
	return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

func TestPcbAntennaKeepoutStopsAfterFirstUnverifiedWrite(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("failure-at-%d", failAt), func(t *testing.T) {
			writes := 0
			var components []any
			for i := 0; i < 3; i++ {
				offset := float64(i * 1500)
				components = append(components, map[string]any{
					"primitiveId": fmt.Sprintf("module-%d", i), "designator": fmt.Sprintf("U_SYN_%d", i), "manufacturerId": "ESP32-WROOM",
					"bbox": map[string]any{"minX": offset - 500, "minY": -200, "maxX": offset + 500, "maxY": 1000},
					"pads": []any{map[string]any{"x": offset, "y": 600}},
				})
			}
			cfg, _, cleanup := newAutolayoutTestDaemon(t, func(_ int, call autolayoutTestCall) string {
				var result map[string]any
				switch call.Action {
				case "pcb.components.list":
					result = map[string]any{"components": components}
				case "pcb.silk.list":
					result = map[string]any{"texts": []any{}}
				case "pcb.layers.list":
					result = map[string]any{"copperLayerCount": 2}
				case "pcb.region.list":
					result = map[string]any{"regions": []any{}}
				case "pcb.outline.get":
					result = map[string]any{"segments": float64(0), "centerlineBBox": nil}
				case "pcb.region.create":
					writes++
					if writes == failAt {
						return `{"ok":true,"result":{"primitiveId":"retained-partial","partial":true,"verified":false}}`
					}
					points, err := pointsFromJSONPayload(call.Payload["points"])
					if err != nil {
						t.Error(err)
						return `{"ok":false}`
					}
					result = map[string]any{"primitiveId": fmt.Sprintf("verified-%d", writes), "verified": true, "layer": 12, "ruleType": []int{5, 6, 7, 8}, "source": pointsPolygonSource(points)}
				default:
					t.Errorf("unexpected action %s", call.Action)
					return `{"ok":false}`
				}
				raw, err := json.Marshal(map[string]any{"ok": true, "result": result})
				if err != nil {
					t.Error(err)
				}
				return string(raw)
			})
			defer cleanup()
			var out, stderr bytes.Buffer
			cmd := newPcbCmd(cfg, &out, &stderr)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"antenna-keepout", "--window", "w1"})
			if err := cmd.Execute(); err == nil || writes != failAt || !strings.Contains(out.String(), "retained-partial") {
				t.Fatalf("dependent creation continued or evidence lost: err=%v writes=%d stdout=%s", err, writes, out.String())
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["created"] != float64(failAt-1) || result["partial"] != true || result["ok"] != false {
				t.Fatalf("lost completed writes: %s", out.String())
			}
			if failAt == 2 && !strings.Contains(out.String(), "verified-1") {
				t.Fatalf("lost prior successful PID: %s", out.String())
			}
		})
	}
}

func TestAntennaOnBoardRectClipsCoverageToBoard(t *testing.T) {
	board := &layoutBBox{MinX: -240, MinY: -1600, MaxX: 940, MaxY: -343}
	// Live 2026-10-13 plan: 20 mil outward margin crosses the top edge.
	got := antennaOnBoardRect([4]float64{-20, -603, 768, -323}, board)
	if got != [4]float64{-20, -603, 768, -343} {
		t.Fatalf("clipped = %v", got)
	}
	if antennaOnBoardRect([4]float64{1, 2, 3, 4}, nil) != [4]float64{1, 2, 3, 4} {
		t.Fatal("unknown outline must keep the full rect")
	}
	if antennaOnBoardRect([4]float64{2000, 0, 2100, 10}, board) != [4]float64{2000, 0, 2100, 10} {
		t.Fatal("no overlap must keep the full rect, never an empty one")
	}
}

func TestAntennaEdgeSpanWidensOnlyAnEdgeMountedAntenna(t *testing.T) {
	// Demo 2026-10-13 geometry: WROOM-1 top (pad-free) end flush with the top edge.
	bb := layoutBBox{MinX: 0, MinY: -1373, MaxX: 748, MaxY: -342.7}
	strip := [4]float64{-20, -603, 768, -322.6}
	board := &layoutBBox{MinX: -240, MinY: -1600, MaxX: 940, MaxY: -343}
	if got := antennaEdgeSpan(strip, bb, board); got != [4]float64{-240, -603, 940, -322.6} {
		t.Fatalf("edge-mounted antenna must span the board width, got %v", got)
	}
	mid := &layoutBBox{MinX: -240, MinY: -1600, MaxX: 940, MaxY: 400}
	if got := antennaEdgeSpan(strip, bb, mid); got != strip {
		t.Fatalf("mid-board antenna keeps the module-width strip, got %v", got)
	}
	if got := antennaEdgeSpan(strip, bb, nil); got != strip {
		t.Fatal("unknown outline keeps the strip")
	}
	// Antenna pointing left, flush with the left edge: span the board height.
	lb := layoutBBox{MinX: 100, MinY: 0, MaxX: 1130, MaxY: 748}
	left := [4]float64{80, -20, 360, 768}
	lboard := &layoutBBox{MinX: 100, MinY: -200, MaxX: 1500, MaxY: 900}
	if got := antennaEdgeSpan(left, lb, lboard); got != [4]float64{80, -200, 360, 900} {
		t.Fatalf("left-edge antenna must span the board height, got %v", got)
	}
}
