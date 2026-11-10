package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureTestResponse(action string) *actionResult {
	return &actionResult{ID: "request-" + action, WindowID: "window", CreatedAt: time.Now().UTC(), OK: true,
		Context: &actionContext{ProjectUUID: "project", ProjectName: "fixture", DocumentUUID: "board", DocumentType: "pcb", TabID: "tab"}, Result: map[string]any{}}
}

func TestPCBCaptureIdentityRejectsEachObservedSwitch(t *testing.T) {
	for _, key := range []string{"window", "project", "document", "type", "tab"} {
		t.Run(key, func(t *testing.T) {
			c := newPCBCaptureEvidence()
			if err := c.observe("first", time.Now(), captureTestResponse("first"), nil); err != nil {
				t.Fatal(err)
			}
			second := captureTestResponse("second")
			switch key {
			case "window":
				second.WindowID = "other"
			case "project":
				second.Context.ProjectUUID = "other"
			case "document":
				second.Context.DocumentUUID = "other"
			case "type":
				second.Context.DocumentType = "schematic"
			case "tab":
				second.Context.TabID = "other"
			}
			if err := c.observe("second", time.Now(), second, nil); err == nil {
				t.Fatal("mixed identity accepted")
			}
			if c.IdentityVerified || c.Requests[1].Status != "identity-mismatch" {
				t.Fatalf("capture=%+v", c)
			}
		})
	}
}

func TestPCBCaptureUnknownIdentityNeverBecomesVerified(t *testing.T) {
	c := newPCBCaptureEvidence()
	if err := c.observe("old", time.Now(), &actionResult{OK: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.observe("new", time.Now(), captureTestResponse("new"), nil); err != nil {
		t.Fatal(err)
	}
	if c.IdentityVerified || len(c.Missing) == 0 || len(c.Requests) != 2 {
		t.Fatalf("capture=%+v", c)
	}
}

func captureFixture(t *testing.T, mutate func(string, map[string]any)) *appConfig {
	t.Helper()
	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"service":"easyeda-agent","windows":[{"windowId":"window"}]}`))
			return
		}
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		n++
		results := map[string]map[string]any{
			"pcb.components.list": {"components": []any{}}, "pcb.outline.get": {"bbox": map[string]any{"minX": 10, "minY": 20, "maxX": 200, "maxY": 100}},
			"pcb.line.list": {"lines": []any{}, "arcs": []any{}, "arcsAvailable": true}, "pcb.silk.list": {"texts": []any{}},
			"pcb.drc.rules": {"rules": nil}, "pcb.layers.list": {"copperLayerCount": 2},
			"pcb.via.list": {"vias": []any{}}, "pcb.pour.list": {"pours": []any{}}, "pcb.poured.list": {"poured": []any{}}, "pcb.region.list": {"regions": []any{}}, "pcb.fill.list": {"fills": []any{}},
			"pcb.nets.list": {"nets": []any{}}, "pcb.drc.check": {"passed": false, "count": 2}, "pcb.snapshot": {},
		}
		response := map[string]any{"id": fmt.Sprintf("request-%d", n), "windowId": "window", "createdAt": time.Now().UTC(), "ok": true,
			"context": map[string]any{"projectUuid": "project", "projectName": "fixture", "documentUuid": "board", "documentType": "pcb", "tabId": "tab"}, "result": results[req.Action]}
		if mutate != nil {
			mutate(req.Action, response)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	host, port, _ := strings.Cut(strings.TrimPrefix(server.URL, "http://"), ":")
	return &appConfig{host: host, ports: port + "-" + port}
}

func TestBoardSnapshotCaptureKnownEmptyAndUnknownRemainDistinct(t *testing.T) {
	cfg := captureFixture(t, func(action string, r map[string]any) {
		if action == "pcb.via.list" {
			r["result"] = map[string]any{}
		}
	})
	s, err := fetchBoardSnapshot(cfg, "window", boardSnapshotOpts{withSilk: true, withRules: true, withLayers: true, withCopper: true})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Capture.IdentityVerified || s.Capture.DocumentUUID != "board" || s.Capture.CompletedAt == "" {
		t.Fatalf("capture=%+v", s.Capture)
	}
	if s.Components == nil || s.Silk == nil || s.Availability["silk"] != "available" {
		t.Fatal("known empty lost")
	}
	if s.Copper.Availability["vias"] != "unknown" || s.Copper.Vias != nil {
		t.Fatal("missing inventory became empty")
	}
	if s.Rules != nil || s.Availability["rules"] != "unknown" {
		t.Fatal("default rules published as measured")
	}
	original := s.SemanticSHA256
	s.Capture = captureCloneForTest(s.Capture)
	s.Capture.StartedAt = "different observation"
	hash, err := boardSnapshotSemanticSHA256(s)
	if err != nil || hash != original {
		t.Fatalf("provenance changes semantic hash: %s %v", hash, err)
	}
}
func captureCloneForTest(c *pcbCaptureEvidence) *pcbCaptureEvidence { clone := *c; return &clone }

func TestBoardSnapshotCaptureRefusesMixedDocument(t *testing.T) {
	cfg := captureFixture(t, func(action string, r map[string]any) {
		if action == "pcb.silk.list" {
			r["context"].(map[string]any)["documentUuid"] = "other"
		}
	})
	s, err := fetchBoardSnapshot(cfg, "window", boardSnapshotOpts{withSilk: true, withLayers: true})
	if err == nil || s == nil || s.Capture.IdentityVerified {
		t.Fatalf("mixed capture accepted: snapshot=%+v error=%v", s, err)
	}
	if s.Silk != nil {
		t.Fatal("foreign document silk was merged")
	}
}

func TestPcbSilkLineWidthUnknownAndMeasuredZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   *float64
	}{
		{"old", map[string]any{}, nil}, {"unknown-zero", map[string]any{"lineWidth": 0, "lineWidthAvailability": "unknown"}, nil},
		{"measured", map[string]any{"lineWidth": 6.0, "lineWidthAvailability": "available", "lineWidthSource": "getState_LineWidth"}, ptrCaptureFloat(6)},
		{"measured-zero", map[string]any{"lineWidth": 0.0, "lineWidthAvailability": "available"}, ptrCaptureFloat(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			texts, err := parsePcbSilkTexts(map[string]any{"texts": []any{tc.fields}})
			if err != nil {
				t.Fatal(err)
			}
			got := texts[0].LineWidth
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("line width=%v", got)
			}
		})
	}
	if _, err := parsePcbSilkTexts(map[string]any{}); err == nil {
		t.Fatal("missing inventory accepted")
	}
}
func ptrCaptureFloat(v float64) *float64 { return &v }

func writeCapturePNG(t *testing.T) (string, string) {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			im.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: uint8((x + y) * 3), A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "capture.png")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, sha256Hex(b.Bytes())
}

func TestPcbStageCaptureIntegrityAndDuplicateSemantics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fault    string
		previous bool
		wantErr  bool
	}{
		{"valid", "", false, false}, {"fresh-identical-connector-flag", "stale", false, false}, {"legacy-duplicate", "", true, true},
		{"missing", "missing", false, true}, {"corrupt", "corrupt", false, true}, {"reported-hash", "hash", false, true},
		{"artifact-hash", "artifact-hash", false, true}, {"identity-switch", "identity", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, sha := writeCapturePNG(t)
			if tc.fault == "corrupt" {
				if err := os.WriteFile(p, []byte("not png"), 0o644); err != nil {
					t.Fatal(err)
				}
				sha = sha256Hex([]byte("not png"))
			}
			cfg := captureFixture(t, func(action string, r map[string]any) {
				if action == "pcb.snapshot" {
					r["result"] = map[string]any{"sha256": sha, "stale": tc.fault == "stale"}
					if tc.fault != "missing" {
						r["artifacts"] = []any{map[string]any{"path": p, "mimeType": "image/png", "sha256": sha}}
					}
					if tc.fault == "hash" {
						r["result"].(map[string]any)["sha256"] = "wrong"
					}
					if tc.fault == "artifact-hash" {
						r["artifacts"].([]any)[0].(map[string]any)["sha256"] = "wrong"
					}
				}
				if action == "pcb.via.list" && tc.fault == "identity" {
					r["context"].(map[string]any)["documentUuid"] = "other"
				}
			})
			dir := filepath.Join(t.TempDir(), "stage")
			prev := ""
			if tc.previous {
				prev = sha
			}
			var out, stderr bytes.Buffer
			err := capturePcbStage(cfg, "window", "fixture", dir, "board", prev, false, &out, &stderr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v out=%s", err, out.String())
			}
			raw, e := os.ReadFile(filepath.Join(dir, "stage.json"))
			if e != nil {
				t.Fatal(e)
			}
			var m map[string]any
			if e = json.Unmarshal(raw, &m); e != nil {
				t.Fatal(e)
			}
			if tc.fault == "stale" && m["stale"] != false {
				t.Fatal("fresh identical pixels falsely rejected")
			}
			if tc.fault == "identity" {
				if _, err := os.Stat(filepath.Join(dir, "vias.json")); err == nil {
					t.Fatal("foreign data persisted")
				}
			}
			if !tc.wantErr {
				hashes := m["fileHashes"].(map[string]any)
				for name, want := range hashes {
					b, e := os.ReadFile(filepath.Join(dir, name))
					if e != nil || sha256Hex(b) != want {
						t.Fatalf("unverifiable %s: %v", name, e)
					}
				}
			}
		})
	}
}

func TestBoardSnapshotRequestedWindowMismatch(t *testing.T) {
	cfg := captureFixture(t, nil)
	if _, err := fetchBoardSnapshot(cfg, "different-requested-window", boardSnapshotOpts{}); err == nil {
		t.Fatal("wrong response window accepted")
	}
}

func TestPcbSilkVisibilityIsExplicitAndIndependent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		raw            map[string]any
		known, visible bool
	}{
		{"visible", map[string]any{"visible": true, "visibilityAvailability": "available"}, true, true},
		{"hidden", map[string]any{"visible": false, "visibilityAvailability": "available"}, true, false},
		{"legacy", map[string]any{"bbox": map[string]any{"minX": 0, "minY": 0, "maxX": 40, "maxY": 40}}, false, false},
		{"unreadable", map[string]any{"visible": true, "visibilityAvailability": "unknown", "visibilityError": "getter threw"}, false, false},
		{"wrong type", map[string]any{"visible": 0, "visibilityAvailability": "available"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.raw["keyVisible"] = true
			tc.raw["keyVisibilityAvailability"] = "available"
			texts, err := parsePcbSilkTexts(map[string]any{"texts": []any{tc.raw}})
			if err != nil {
				t.Fatal(err)
			}
			got := texts[0]
			if (got.Visible != nil) != tc.known || (got.Visible != nil && *got.Visible != tc.visible) {
				t.Fatalf("unexpected visible %v", got.Visible)
			}
			if got.KeyVisible == nil || !*got.KeyVisible || got.KeyVisibilityAvailability != "available" {
				t.Fatal("key visibility lost")
			}
			if !tc.known && got.VisibilityAvailability != "unknown" {
				t.Fatal("unknown visibility accepted")
			}
		})
	}
}

func TestPCBCaptureUnorderedResponseCannotVerifyIdentity(t *testing.T) {
	capture := newPCBCaptureEvidence()
	response := captureTestResponse("unordered")
	response.Seq = schSeqCounters{Known: true, Seq: 10, SeqAbandoned: 0, Unordered: true}
	if err := capture.observe("unordered", time.Now(), response, nil); err != nil {
		t.Fatal(err)
	}
	if capture.IdentityVerified || len(capture.Missing) != 1 || !strings.Contains(capture.Missing[0], "unordered response boundary") {
		t.Fatalf("unordered response verified: %+v", capture)
	}
	if len(capture.Requests) != 1 || !capture.Requests[0].Sequence.Unordered {
		t.Fatal("unordered diagnostic evidence was discarded")
	}
}

func TestWriteBoardSnapshotFileHashesPersistedBytes(t *testing.T) {
	for _, data := range []string{
		`{ "components":[], "capturedAt":"2026-01-01T00:00:00Z", "capture":{"projectUuid":"project","documentUuid":"board","identityVerified":false,"missing":["unknown source"]}, "availability":{"silk":"unknown"}, "partial":["silk unreadable"] }`,
		"{\n  \"components\": []\n}\n",
	} {
		// Keep intentional whitespace: file hashes must bind bytes, not re-marshaled JSON.
		path := filepath.Join(t.TempDir(), "board.json")
		if err := writeBoardSnapshotFile(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		persisted, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(persisted) != data {
			t.Fatal("dump bytes changed")
		}
		receiptRaw, err := os.ReadFile(path + ".capture.json")
		if err != nil {
			t.Fatal(err)
		}
		var receipt map[string]any
		if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt["sha256"] != sha256Hex(persisted) || receipt["bytes"] != float64(len(persisted)) || receipt["snapshot"] != "board.json" {
			t.Fatalf("wrong byte receipt: %+v", receipt)
		}
		if strings.Contains(data, "unknown source") {
			capture := receipt["capture"].(map[string]any)
			if capture["projectUuid"] != "project" || capture["documentUuid"] != "board" || capture["identityVerified"] != false {
				t.Fatal("capture evidence changed")
			}
			if receipt["availability"].(map[string]any)["silk"] != "unknown" || len(receipt["partial"].([]any)) != 1 {
				t.Fatal("missing observations lost")
			}
		}
	}
}

func TestWriteBoardSnapshotFileReportsReceiptFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.Mkdir(path+".capture.json", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeBoardSnapshotFile(path, []byte(`{"components":[]}`)); err == nil {
		t.Fatal("failed receipt reported success")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err := writeBoardSnapshotFile(invalid, []byte("not JSON")); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	if _, err := os.Stat(invalid); !os.IsNotExist(err) {
		t.Fatal("invalid snapshot created output")
	}
}

func TestPcbDumpSidecarOnlyWithOutputFile(t *testing.T) {
	cfg := captureFixture(t, nil)
	window := "window"
	for _, toFile := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		command := newPcbDumpCmd(cfg, &window, &stdout, &stderr)
		args := []string{"--no-silk", "--no-rules", "--no-layers"}
		path := filepath.Join(t.TempDir(), "board.json")
		if toFile {
			args = append(args, "--out", path)
		}
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		if toFile {
			if stdout.Len() != 0 {
				t.Fatal("file mode unexpectedly wrote stdout")
			}
			if _, err := os.Stat(path + ".capture.json"); err != nil {
				t.Fatal(err)
			}
		} else {
			var snapshot boardSnapshot
			if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
				t.Fatalf("stdout no longer contains just snapshot JSON: %v", err)
			}
			if snapshot.Components == nil {
				t.Fatal("stdout inventory changed")
			}
			if _, err := os.Stat(path + ".capture.json"); !os.IsNotExist(err) {
				t.Fatal("stdout mode wrote sidecar")
			}
		}
	}
}
