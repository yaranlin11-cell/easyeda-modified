package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zhoushoujianwork/easyeda-agent/internal/spec"
)

func writeEvidence(w io.Writer, r spec.EvidenceReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return err
	}
	if r.Status != "pass" {
		return fmt.Errorf("evidence check: %s (%d finding(s)); no EDA writes", r.Status, len(r.Issues))
	}
	return nil
}
func readEvidence(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return spec.DecodeEvidence(b, v)
}

func newSpecConstraintsCheckCmd(out io.Writer) *cobra.Command {
	var from, observed string
	c := &cobra.Command{Use: "constraints-check", Short: "Compare source-bound numeric constraints with supplied measurements", Long: "Offline numeric comparison only. Supports explicit minimum, maximum, range and reference-only rules. Scope, subject, metric, stage and compatible units must match. No automatic CAD extraction or manufacturing certification. Missing measurements return incomplete; invalid data or violations return fail.", Example: "  easyeda spec constraints-check --from constraints.json --observed measurements.json", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var want spec.ConstraintSet
		var got spec.MeasurementSet
		if err := readEvidence(from, &want); err != nil {
			return err
		}
		if err := readEvidence(observed, &got); err != nil {
			return err
		}
		return writeEvidence(out, spec.CheckConstraints(want, got, filepath.Dir(from), filepath.Dir(observed)))
	}}
	c.Flags().StringVar(&from, "from", "", "source-bound numeric constraints JSON (required)")
	c.Flags().StringVar(&observed, "observed", "", "source-bound measurements JSON (required)")
	_ = c.MarkFlagRequired("from")
	_ = c.MarkFlagRequired("observed")
	return c
}
func newSpecSourceProbeCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{Use: "source-probe <file>", Short: "Inspect source bytes and preview coverage without claiming full extraction", Long: "Read-only local format/hash probe. Allegro embedded previews do not expose a complete board. No EDA import or network upload.", Example: "  easyeda spec source-probe reference.brd", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, e := spec.ProbeSource(args[0])
		if e != nil {
			return e
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}}
}
func newSpecReferenceCheckCmd(out io.Writer) *cobra.Command {
	var from string
	c := &cobra.Command{Use: "reference-check", Short: "Validate a source-bound reference manifest and report missing evidence", Long: "Offline input-readiness only; does not approve or modify a design. Resolves source paths relative to the manifest. Fail/incomplete returns nonzero. Unknown JSON fields are rejected.", Example: "  easyeda spec reference-check --from reference.json", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var m spec.ReferenceManifest
		if e := readEvidence(from, &m); e != nil {
			return e
		}
		return writeEvidence(out, spec.CheckReference(m, filepath.Dir(from)))
	}}
	c.Flags().StringVar(&from, "from", "", "schemaVersion 1 reference manifest JSON (required)")
	_ = c.MarkFlagRequired("from")
	return c
}
func newSpecChainCheckCmd(out io.Writer) *cobra.Command {
	var from, observed string
	c := &cobra.Command{Use: "chain-check", Short: "Check supplied physical chains for opens, bypassed joints, order and cross-chain shorts", Long: "Offline graph proof only. Independent intent and source-bound observed graph are required. Full board copper (including pours), module connections and joints must be accounted for. Does not infer a graph from net names, PCB images or DRC. All declared chains are isolated. Missing evidence returns incomplete.", Example: "  easyeda spec chain-check --from chains.json --observed graph.json", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var m spec.ChainIntent
		var g spec.ChainGraph
		if e := readEvidence(from, &m); e != nil {
			return e
		}
		if e := readEvidence(observed, &g); e != nil {
			return e
		}
		r := spec.CheckChains(m, g)
		for _, s := range g.Sources {
			path := s.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(observed), path)
			}
			h, e := spec.FileSHA(path)
			if e != nil || h != s.SHA256 {
				r.Status = "fail"
				r.Issues = append(r.Issues, spec.EvidenceIssue{Code: "source-binding", Object: s.ID, Detail: "source unavailable or SHA256 mismatch"})
			}
		}
		return writeEvidence(out, r)
	}}
	c.Flags().StringVar(&from, "from", "", "independent chain intent JSON (required)")
	c.Flags().StringVar(&observed, "observed", "", "complete physical graph JSON with bound source files (required)")
	_ = c.MarkFlagRequired("from")
	_ = c.MarkFlagRequired("observed")
	return c
}
