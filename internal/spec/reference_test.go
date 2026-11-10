package spec

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceProbeDoesNotPromotePreviewToBoard(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{2, 9, 0x14, 0})
	b.WriteString("allegro 17.4 S040\x00")
	w := zip.NewWriter(&b)
	w.SetOffset(int64(b.Len()))
	f, _ := w.Create("../../private.tmp")
	f.Write([]byte(`{"design":{"comps":[{"class":"BOARD GEOMETRY","subclass":"OUTLINE","points":[]}]}}`))
	w.Close()
	p := filepath.Join(t.TempDir(), "reference.brd")
	os.WriteFile(p, b.Bytes(), 0600)
	got, e := ProbeSource(p)
	if e != nil {
		t.Fatal(e)
	}
	if got.Format != "allegro-binary" || got.ExtractionStatus != "partial" || got.PreviewRecords != 1 {
		t.Fatalf("wrong coverage: %+v", got)
	}
	if _, e := os.Stat(filepath.Join(filepath.Dir(p), "private.tmp")); !os.IsNotExist(e) {
		t.Fatal("archive path extracted")
	}
}
func referenceFixture(t *testing.T) (ReferenceManifest, string) {
	t.Helper()
	base := t.TempDir()
	p := filepath.Join(base, "input.txt")
	os.WriteFile(p, []byte("synthetic source"), 0600)
	h, _ := FileSHA(p)
	return ReferenceManifest{SchemaVersion: 1, Mode: "reference-assisted", Scopes: []string{"module", "carrier"}, Sources: []ReferenceSource{{ID: "sch", Path: "input.txt", SHA256: h, Role: "schematic-reference", Scope: "module", Revision: "1", ReadStatus: "complete"}}, Requirements: []ReferenceRequirement{{ID: "chain", Scope: "module", Kind: "topology", Statement: "Independent synthetic continuity chain", Evidence: []EvidenceRef{{SourceID: "sch", Locator: "line 1"}}, Resolution: "verified"}}, Capabilities: []ReferenceCapability{{ID: "capture", Required: true, Status: "live-verified", Evidence: "synthetic test declaration"}}}, base
}
func TestReferenceReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		edit         func(*ReferenceManifest)
	}{
		{"ready", "pass", func(m *ReferenceManifest) {}},
		{"partial input", "incomplete", func(m *ReferenceManifest) { m.Sources[0].ReadStatus = "partial" }},
		{"no capabilities", "incomplete", func(m *ReferenceManifest) { m.Capabilities = nil }},
		{"unsupported", "incomplete", func(m *ReferenceManifest) { m.Capabilities[0].Status = "unsupported" }},
		{"assumption", "incomplete", func(m *ReferenceManifest) { m.Requirements[0].Resolution = "assumed" }},
		{"no requirements", "incomplete", func(m *ReferenceManifest) { m.Requirements = nil }},
		{"manufacturing is not a circuit", "incomplete", func(m *ReferenceManifest) { m.Requirements[0].Kind = "manufacturing" }},
		{"source changed", "fail", func(m *ReferenceManifest) { m.Sources[0].SHA256 = strings.Repeat("0", 64) }},
		{"scope confusion", "fail", func(m *ReferenceManifest) { m.Requirements[0].Scope = "other" }},
		{"missing provenance", "fail", func(m *ReferenceManifest) { m.Requirements[0].Evidence[0].SourceID = "missing" }},
		{"duplicate source", "fail", func(m *ReferenceManifest) { m.Sources = append(m.Sources, m.Sources[0]) }},
		{"unknown mode", "fail", func(m *ReferenceManifest) { m.Mode = "auto" }},
		{"no reference", "fail", func(m *ReferenceManifest) { m.Sources[0].Role = "manufacturing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, base := referenceFixture(t)
			tc.edit(&m)
			r := CheckReference(m, base)
			if r.Status != tc.status {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestEvidenceDecodeRejectsTyposAndTrailing(t *testing.T) {
	for _, raw := range []string{`{"schemaVerison":1}`, `{"schemaVersion":1} {}`, `{"schemaVersion":1} trailing`} {
		var m ReferenceManifest
		if DecodeEvidence([]byte(raw), &m) == nil {
			t.Fatal("invalid input accepted", raw)
		}
	}
}
