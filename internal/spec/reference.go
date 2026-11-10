package spec

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EvidenceReport distinguishes schema errors from missing design evidence.
type EvidenceReport struct {
	Status      string          `json:"status"`
	Scope       string          `json:"scope"`
	Issues      []EvidenceIssue `json:"issues"`
	Limitations []string        `json:"limitations"`
}
type EvidenceIssue struct {
	Code   string `json:"code"`
	Object string `json:"object"`
	Detail string `json:"detail"`
}

func (r *EvidenceReport) issue(code, object, detail string) {
	r.Issues = append(r.Issues, EvidenceIssue{code, object, detail})
}
func DecodeEvidence(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("exactly one JSON document required")
	}
	return nil
}
func FileSHA(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

type SourceProbe struct {
	SHA256           string         `json:"sha256"`
	Bytes            int            `json:"bytes"`
	Format           string         `json:"format"`
	ExtractionStatus string         `json:"extractionStatus"`
	Markers          []string       `json:"markers"`
	PreviewRecords   int            `json:"previewRecords"`
	PreviewClasses   map[string]int `json:"previewClasses,omitempty"`
	Limitations      []string       `json:"limitations"`
}

func ProbeSource(path string) (SourceProbe, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return SourceProbe{}, e
	}
	h := sha256.Sum256(b)
	p := SourceProbe{SHA256: hex.EncodeToString(h[:]), Bytes: len(b), Format: "unknown", ExtractionStatus: "unreadable", Markers: []string{}, Limitations: []string{"format probe only; no netlist, stackup, geometry completeness or design acceptance proof"}}
	if bytes.HasPrefix(b, []byte("%PDF-")) {
		p.Format = "pdf"
		p.ExtractionStatus = "partial"
		p.Limitations = append(p.Limitations, "text and rendered pages must be independently inspected; glyphs are not pin identities")
		return p, nil
	}
	// Allegro 17.x magic corroborated by an actual product-version marker.
	markers := regexp.MustCompile(`allegro [0-9]+\.[0-9]+[^\x00\r\n]{0,80}`).FindAll(b, 4)
	if len(b) > 4 && b[2] == 0x14 && b[3] == 0 && len(markers) > 0 {
		p.Format = "allegro-binary"
		for _, m := range markers {
			p.Markers = append(p.Markers, string(m))
		}
	}
	z, ze := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if ze == nil {
		if p.Format == "unknown" {
			p.Format = "zip"
		}
		p.ExtractionStatus = "partial"
		for _, f := range z.File {
			if f.UncompressedSize64 > 16<<20 {
				continue
			}
			rd, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(rd, 16<<20+1))
			rd.Close()
			if err != nil || len(data) > 16<<20 {
				continue
			}
			var preview struct {
				Design struct {
					Comps []map[string]any `json:"comps"`
				} `json:"design"`
			}
			if json.Unmarshal(data, &preview) != nil || len(preview.Design.Comps) == 0 {
				continue
			}
			if p.PreviewClasses == nil {
				p.PreviewClasses = map[string]int{}
			}
			for _, c := range preview.Design.Comps {
				class, _ := c["class"].(string)
				sub, _ := c["subclass"].(string)
				if class == "" {
					continue
				}
				p.PreviewRecords++
				p.PreviewClasses[class+"/"+sub]++
			}
		}
	}
	if p.Format == "allegro-binary" {
		p.Limitations = append(p.Limitations, "embedded ZIP/preview is not a complete board database; use a validated native export/converter before connectivity comparison")
	}
	return p, nil
}

type ReferenceSource struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Role       string `json:"role"`
	Scope      string `json:"scope"`
	Revision   string `json:"revision"`
	ReadStatus string `json:"readStatus"`
}
type EvidenceRef struct {
	SourceID string `json:"sourceId"`
	Locator  string `json:"locator"`
}
type ReferenceRequirement struct {
	ID         string        `json:"id"`
	Scope      string        `json:"scope"`
	Kind       string        `json:"kind"`
	Statement  string        `json:"statement"`
	Evidence   []EvidenceRef `json:"evidence"`
	Resolution string        `json:"resolution"`
}
type ReferenceCapability struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}
type ReferenceQuestion struct {
	ID       string `json:"id"`
	Detail   string `json:"detail"`
	Blocking bool   `json:"blocking"`
}
type ReferenceManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Mode          string                 `json:"mode"`
	Scopes        []string               `json:"scopes"`
	Sources       []ReferenceSource      `json:"sources"`
	Requirements  []ReferenceRequirement `json:"requirements"`
	Capabilities  []ReferenceCapability  `json:"capabilities"`
	Unresolved    []ReferenceQuestion    `json:"unresolved"`
}

func oneOf(v string, choices ...string) bool {
	for _, c := range choices {
		if v == c {
			return true
		}
	}
	return false
}
func CheckReference(m ReferenceManifest, base string) EvidenceReport {
	r := EvidenceReport{Status: "pass", Scope: "offline-input-readiness", Issues: []EvidenceIssue{}, Limitations: []string{"valid manifest is not verified extraction, design approval or live completion", "capability and resolution fields are caller declarations, not automatic observations"}}
	bad := func(id, msg string) { r.issue("invalid", id, msg); r.Status = "fail" }
	missing := func(id, msg string) {
		r.issue("incomplete", id, msg)
		if r.Status != "fail" {
			r.Status = "incomplete"
		}
	}
	if m.SchemaVersion != 1 {
		bad("schemaVersion", "expected 1")
	}
	if !oneOf(m.Mode, "demo", "reference-assisted", "independent") {
		bad("mode", "explicit supported task mode required")
	}
	scopes := map[string]bool{}
	for _, s := range m.Scopes {
		if strings.TrimSpace(s) == "" || scopes[s] {
			bad(s, "empty/duplicate scope")
		}
		scopes[s] = true
	}
	if len(scopes) == 0 {
		bad("scopes", "at least one physical scope required")
	}
	sources := map[string]bool{}
	roles := map[string]bool{}
	for _, s := range m.Sources {
		if s.ID == "" || sources[s.ID] {
			bad(s.ID, "empty/duplicate source ID")
		}
		sources[s.ID] = true
		if !scopes[s.Scope] {
			bad(s.ID, "unknown source scope")
		}
		if !oneOf(s.Role, "requirements", "manufacturing", "schematic-reference", "pcb-reference") {
			bad(s.ID, "invalid source role")
		}
		roles[s.Role] = true
		if !oneOf(s.ReadStatus, "complete", "partial", "unreadable") {
			bad(s.ID, "explicit readStatus required")
		} else if s.ReadStatus != "complete" {
			missing(s.ID, "source extraction is "+s.ReadStatus)
		}
		if s.Revision == "" {
			missing(s.ID, "revision unrecorded")
		}
		path := s.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		hash, err := FileSHA(path)
		if s.Path == "" || err != nil {
			missing(s.ID, "source file unavailable")
		} else if hash != s.SHA256 {
			bad(s.ID, "source SHA256 mismatch")
		}
	}
	if len(m.Sources) == 0 {
		bad("sources", "source evidence required")
	}
	if m.Mode == "reference-assisted" && !roles["schematic-reference"] && !roles["pcb-reference"] {
		bad("sources", "reference-assisted mode requires a reference source")
	}
	ids := map[string]bool{}
	for _, q := range m.Requirements {
		if q.ID == "" || ids[q.ID] {
			bad(q.ID, "empty/duplicate requirement")
		}
		ids[q.ID] = true
		if !scopes[q.Scope] || q.Statement == "" || !oneOf(q.Kind, "functional", "electrical", "mechanical", "manufacturing", "topology") {
			bad(q.ID, "scope, kind and statement required")
		}
		if len(q.Evidence) == 0 {
			missing(q.ID, "no source evidence")
		}
		for _, e := range q.Evidence {
			if !sources[e.SourceID] || strings.TrimSpace(e.Locator) == "" {
				bad(q.ID, "unknown source or empty evidence locator")
			}
		}
		if !oneOf(q.Resolution, "verified", "assumed", "unresolved") {
			bad(q.ID, "invalid resolution")
		} else if q.Resolution != "verified" {
			missing(q.ID, "requirement interpretation is "+q.Resolution)
		}
	}
	hasFunction := false
	for _, q := range m.Requirements {
		if q.Kind == "functional" || q.Kind == "topology" {
			hasFunction = true
		}
	}
	if m.Mode != "demo" && !hasFunction {
		missing("requirements", "manufacturing constraints alone do not define a circuit")
	}
	if len(m.Requirements) == 0 {
		missing("requirements", "no requirements captured")
	}
	ids = map[string]bool{}
	for _, c := range m.Capabilities {
		if c.ID == "" || ids[c.ID] {
			bad(c.ID, "empty/duplicate capability")
		}
		ids[c.ID] = true
		if !oneOf(c.Status, "live-verified", "offline-verified", "unsupported", "unknown") {
			bad(c.ID, "invalid capability status")
		}
		if c.Required && (c.Status != "live-verified" || c.Evidence == "") {
			missing(c.ID, "required capability lacks live evidence")
		}
	}
	if len(m.Capabilities) == 0 {
		missing("capabilities", "capability inventory missing")
	}
	for _, q := range m.Unresolved {
		if q.ID == "" || q.Detail == "" {
			bad(q.ID, "unresolved question requires ID and detail")
		}
		if q.Blocking {
			missing(q.ID, q.Detail)
		}
	}
	return r
}
