package app

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectArchiveValidationAndNoOverwrite(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("project.epru")
	w.Write([]byte("native fixture"))
	z.Close()
	value := map[string]any{"uuid": "project-1", "format": "epro2", "size": float64(b.Len()), "base64": base64.StdEncoding.EncodeToString(b.Bytes())}
	out := filepath.Join(t.TempDir(), "nested", "test.epro2")
	report, err := writeProjectArchive(value, out)
	if err != nil {
		t.Fatal(err)
	}
	if report["restoreVerified"] != false || report["zipIntegrityVerified"] != true {
		t.Fatal(report)
	}
	if _, err = writeProjectArchive(value, out); err == nil {
		t.Fatal("overwrote existing archive")
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, b.Bytes()) {
		t.Fatal("archive bytes changed")
	}
	value["size"] = float64(1)
	if _, err = writeProjectArchive(value, out+"2"); err == nil {
		t.Fatal("accepted inconsistent size")
	}
	value["size"] = float64(3)
	value["base64"] = base64.StdEncoding.EncodeToString([]byte("bad"))
	if _, err = writeProjectArchive(value, out+"3"); err == nil {
		t.Fatal("accepted non-ZIP")
	}
}

func TestProjectTransferCLIRejectsUnsafeOrAmbiguousArguments(t *testing.T) {
	for _, args := range [][]string{
		{"project", "open", "--project-uuid", "p", "--window", "w"},
		{"project", "open", "--project-uuid", "p", "--uuid", "d", "--allow-discard-unsaved", "--window", "w"},
		{"project", "export", "--project-uuid", "p", "--window", "w", "--out", "bad.zip"},
		{"project", "export", "--project-uuid", "p", "--out", filepath.Join(t.TempDir(), "a.epro2")},
	} {
		var out, err bytes.Buffer
		if Run(args, &out, &err) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestProjectTransferDispatchesTypedActions(t *testing.T) {
	for _, operation := range []string{"open", "export"} {
		t.Run(operation, func(t *testing.T) {
			for _, missing := range []bool{false, true} {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/health" {
						w.Write([]byte(`{"service":"easyeda-agent","windows":[{"windowId":"w"}]}`))
						return
					}
					var req struct {
						Action   string         `json:"action"`
						WindowID string         `json:"windowId"`
						Payload  map[string]any `json:"payload"`
					}
					json.NewDecoder(r.Body).Decode(&req)
					calls++
					if req.Action != "project."+operation || req.WindowID != "w" || req.Payload["projectUuid"] != "p" {
						t.Errorf("unexpected request: %+v", req)
					}
					if _, ok := req.Payload["code"]; ok {
						t.Error("script payload")
					}
					if operation == "open" && (req.Payload["allowDiscardUnsaved"] != true || req.Payload["pageUuid"] != "page") {
						t.Errorf("missing open guards: %+v", req)
					}
					if missing {
						json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": "UNKNOWN_ACTION", "message": "upgrade connector"}})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"uuid": "p"}})
				}))
				host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
				_, _, err := projectTransferRequest(&appConfig{host: host, ports: port + "-" + port}, "w", operation, "p", "page")
				srv.Close()
				if (err != nil) != missing {
					t.Fatalf("error=%v missing=%v", err, missing)
				}
				if calls != 1 {
					t.Fatalf("unexpected retries or fallback: %d", calls)
				}
			}
		})
	}
}

func TestProjectImportValidatesAndDispatchesNewProjectOnly(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("project.epru")
	w.Write([]byte("native fixture"))
	z.Close()
	good := filepath.Join(dir, "good.epro2")
	os.WriteFile(good, b.Bytes(), 0644)
	var nb bytes.Buffer
	nz := zip.NewWriter(&nb)
	nw, _ := nz.Create("blink/blink.eprj3")
	nw.Write([]byte("{}"))
	nz.Close()
	noEpru := filepath.Join(dir, "folder.epro2")
	os.WriteFile(noEpru, nb.Bytes(), 0644)
	packed := filepath.Join(dir, "folder.zip")
	os.WriteFile(packed, nb.Bytes(), 0644)
	notZip := filepath.Join(dir, "bad.epro2")
	os.WriteFile(notZip, []byte("bad"), 0644)

	var reqs []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"service":"easyeda-agent","windows":[{"windowId":"w"}]}`))
			return
		}
		var req struct {
			Action  string         `json:"action"`
			Payload map[string]any `json:"payload"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Action != "project.import" {
			t.Errorf("unexpected action %q", req.Action)
		}
		reqs = append(reqs, req.Payload)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"uuid": "new", "created": true}})
	}))
	defer srv.Close()
	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	run := func(args ...string) int {
		var out, errb bytes.Buffer
		return Run(append([]string{"--host", host, "--ports", port + "-" + port, "project", "import"}, args...), &out, &errb)
	}
	for _, bad := range [][]string{
		{"--window", "w", "--file", good},                                       // no name
		{"--file", good, "--name", "n"},                                         // no window
		{"--window", "w", "--file", filepath.Join(dir, "a.txt"), "--name", "n"}, // extension
		{"--window", "w", "--file", notZip, "--name", "n"},                      // not a ZIP
		{"--window", "w", "--file", noEpru, "--name", "n"},                      // epro2 without epru
	} {
		if run(bad...) == 0 {
			t.Fatalf("accepted %v", bad)
		}
	}
	if len(reqs) != 0 {
		t.Fatalf("invalid input reached the connector: %d", len(reqs))
	}
	if run("--window", "w", "--file", good, "--name", "n", "--team-uuid", "team") != 0 {
		t.Fatal("rejected valid epro2")
	}
	if run("--window", "w", "--file", packed, "--name", "n2", "--extract-libraries") != 0 {
		t.Fatal("rejected valid zip packaging")
	}
	if len(reqs) != 2 || reqs[0]["friendlyName"] != "n" || reqs[0]["teamUuid"] != "team" || reqs[0]["importOption"] != nil ||
		reqs[1]["importOption"] != "ImportDocumentExtractLibraries" || reqs[1]["fileName"] != "folder.zip" {
		t.Fatalf("unexpected payloads: %+v", reqs)
	}
	if got, _ := base64.StdEncoding.DecodeString(reqs[0]["base64"].(string)); !bytes.Equal(got, b.Bytes()) {
		t.Fatal("archive bytes changed in transport")
	}
	edif := filepath.Join(dir, "design.EDF")
	edifData := []byte("(edif demo (edifVersion 2 0 0))")
	os.WriteFile(edif, edifData, 0644)
	if run("--window", "w", "--file", edif, "--format", "orcad-edif", "--name", "edif") != 0 {
		t.Fatal("rejected EDIF")
	}
	if len(reqs) != 3 || reqs[2]["fileType"] != "OrCAD" {
		t.Fatalf("wrong EDIF routing: %+v", reqs)
	}
	if got, _ := base64.StdEncoding.DecodeString(reqs[2]["base64"].(string)); !bytes.Equal(got, edifData) {
		t.Fatal("EDIF changed in transport")
	}
	os.WriteFile(edif, []byte("(edif broken"), 0644)
	if run("--window", "w", "--file", edif, "--format", "orcad-edif", "--name", "bad") == 0 || len(reqs) != 3 {
		t.Fatal("truncated EDIF reached connector")
	}
}

func TestProjectEDIFValidation(t *testing.T) {
	for _, text := range []string{"\ufeff\n(edif demo (edifVersion 2 0 0))", "(edif demo (comment \"(text)\"))"} {
		if err := validateProjectEDIF([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{"(edif demo", "(edif demo)(extra)", "(edif demo))", "<library/>", "(edif demo \"oops)", "DSN", "\xd0\xcf\x11\xe0", "(edif " + strings.Repeat("(", 1025) + strings.Repeat(")", 1026)} {
		if validateProjectEDIF([]byte(text)) == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
