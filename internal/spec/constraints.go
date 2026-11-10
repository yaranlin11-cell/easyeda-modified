package spec

import (
	"fmt"
	"math"
	"path/filepath"
)

// Numeric rules describe a named measurement, not a heuristic inferred from a
// drawing. Subject identifies the object or explicit aggregate (e.g. all-vias).
type ConstraintSet struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Sources       []GraphSource       `json:"sources"`
	Rules         []NumericConstraint `json:"rules"`
}
type NumericConstraint struct {
	ID       string      `json:"id"`
	Scope    string      `json:"scope"`
	Subject  string      `json:"subject"`
	Metric   string      `json:"metric"`
	Unit     string      `json:"unit"`
	Stage    string      `json:"stage"` // design or fabricated
	Mode     string      `json:"mode"`  // min, max, range, reference
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
	Evidence EvidenceRef `json:"evidence"`
}
type MeasurementSet struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Sources       []GraphSource           `json:"sources"`
	Measurements  []ConstraintMeasurement `json:"measurements"`
}
type ConstraintMeasurement struct {
	RuleID   string      `json:"ruleId"`
	Scope    string      `json:"scope"`
	Subject  string      `json:"subject"`
	Metric   string      `json:"metric"`
	Unit     string      `json:"unit"`
	Stage    string      `json:"stage"`
	Value    *float64    `json:"value"`
	Evidence EvidenceRef `json:"evidence"`
}

func CheckConstraints(want ConstraintSet, got MeasurementSet, wantDir, gotDir string) EvidenceReport {
	r := EvidenceReport{Status: "pass", Scope: "offline-supplied-numeric-measurements", Issues: []EvidenceIssue{}, Limitations: []string{
		"only declared numeric rules and supplied measurements are compared; no automatic CAD or factory measurement extraction",
		"file hashes bind evidence bytes, not interpretation, aggregate coverage, freshness or fabricated-board conformity",
		"reference-only values do not establish acceptance; material identity, layer order and pin mapping require separate checks",
	}}
	fail := func(id, code, msg string) { r.Status = "fail"; r.issue(code, id, msg) }
	missing := func(id, msg string) {
		if r.Status != "fail" {
			r.Status = "incomplete"
		}
		r.issue("incomplete", id, msg)
	}
	if want.SchemaVersion != 1 || got.SchemaVersion != 1 {
		fail("schemaVersion", "invalid", "expected schemaVersion 1")
	}
	bind := func(srcs []GraphSource, base string) map[string]bool {
		ids := map[string]bool{}
		for _, s := range srcs {
			if s.ID == "" || ids[s.ID] {
				fail(s.ID, "invalid", "empty or duplicate source ID")
			}
			ids[s.ID] = true
			p := s.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(base, p)
			}
			h, e := FileSHA(p)
			if e != nil || s.Path == "" || h != s.SHA256 {
				fail(s.ID, "source-binding", "source unavailable or SHA256 mismatch")
			}
		}
		return ids
	}
	wsrc, gsrc := bind(want.Sources, wantDir), bind(got.Sources, gotDir)
	proof := func(id string, e EvidenceRef, sources map[string]bool) {
		if !sources[e.SourceID] || e.Locator == "" {
			fail(id, "provenance", "known source and nonempty locator required")
		}
	}
	rules := map[string]NumericConstraint{}
	required := 0
	for _, q := range want.Rules {
		_, duplicate := rules[q.ID]
		if q.ID == "" || duplicate {
			fail(q.ID, "invalid", "empty or duplicate rule ID")
		}
		rules[q.ID] = q
		proof(q.ID, q.Evidence, wsrc)
		if q.Scope == "" || q.Subject == "" || q.Metric == "" || !oneOf(q.Stage, "design", "fabricated") {
			fail(q.ID, "invalid", "scope, subject, metric and supported stage required")
		}
		if _, _, ok := numericUnit(q.Unit); !ok {
			fail(q.ID, "invalid", "unsupported unit")
		}
		valid := false
		switch q.Mode {
		case "min":
			valid = q.Min != nil && q.Max == nil
		case "max":
			valid = q.Max != nil && q.Min == nil
		case "range":
			valid = q.Min != nil && q.Max != nil && *q.Min <= *q.Max
		case "reference":
			valid = q.Min == nil && q.Max == nil
			r.issue("reference-only", q.ID, "informational; excluded from acceptance")
		}
		if !valid || !validUnitValue(q.Unit, q.Min) || !validUnitValue(q.Unit, q.Max) {
			fail(q.ID, "invalid", "invalid mode or bounds")
		}
		if q.Mode != "reference" {
			required++
		}
	}
	if required == 0 {
		missing("rules", "no acceptance rules supplied")
	}
	seen := map[string]bool{}
	for _, m := range got.Measurements {
		q, ok := rules[m.RuleID]
		if !ok || seen[m.RuleID] {
			fail(m.RuleID, "invalid", "unknown rule or duplicate measurement")
			continue
		}
		seen[m.RuleID] = true
		proof(m.RuleID, m.Evidence, gsrc)
		if m.Scope != q.Scope || m.Subject != q.Subject || m.Metric != q.Metric || m.Stage != q.Stage {
			fail(m.RuleID, "measurement-mismatch", "scope, subject, metric and stage must match the requirement")
			continue
		}
		dim, factor, valid := numericUnit(m.Unit)
		qdim, qfactor, qvalid := numericUnit(q.Unit)
		if !valid || !qvalid || dim != qdim {
			fail(m.RuleID, "unit-mismatch", "incompatible or unsupported unit")
			continue
		}
		if m.Value == nil {
			missing(m.RuleID, "measurement value unavailable")
			continue
		}
		if !validUnitValue(m.Unit, m.Value) {
			fail(m.RuleID, "invalid", "measurement must be finite and valid for its unit")
			continue
		}
		if q.Mode == "reference" {
			continue
		}
		v := *m.Value * factor / qfactor
		if math.IsInf(v, 0) {
			fail(m.RuleID, "invalid", "unit conversion overflow")
			continue
		}
		// Tiny relative slack only absorbs floating point conversion error; it is
		// not a manufacturing tolerance. Tolerances belong in explicit bounds.
		less := func(a, b float64) bool { return a < b-1e-12*math.Max(1, math.Max(math.Abs(a), math.Abs(b))) }
		if (q.Min != nil && less(v, *q.Min)) || (q.Max != nil && less(*q.Max, v)) {
			fail(m.RuleID, "out-of-range", fmt.Sprintf("measured %.12g %s outside declared bounds", v, q.Unit))
		}
	}
	for _, q := range want.Rules {
		if q.Mode != "reference" && !seen[q.ID] {
			missing(q.ID, "required measurement absent")
		}
	}
	return r
}

func finiteOptional(v *float64) bool { return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0)) }
func validUnitValue(unit string, v *float64) bool {
	if v == nil {
		return true
	}
	if !finiteOptional(v) {
		return false
	}
	switch unit {
	case "count":
		return *v >= 0 && math.Trunc(*v) == *v
	case "percent":
		return *v >= 0 && *v <= 100
	case "mm", "um", "mil":
		return *v >= 0
	case "C":
		return *v >= -273.15
	}
	return false
}
func numericUnit(s string) (string, float64, bool) {
	switch s {
	case "mm":
		return "length", 1, true
	case "um":
		return "length", 0.001, true
	case "mil":
		return "length", 0.0254, true
	case "percent":
		return "percentage", 1, true
	case "count":
		return "count", 1, true
	case "C":
		return "temperature", 1, true
	}
	return "", 0, false
}
