package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// newPcbStageSnapshotCmd builds `easyeda pcb stage-snapshot` — the recording /
// demo stage capture that issue #32 asked for.
//
// WHY a dedicated command: for engineering validation we judge state by DATA
// (list/drc/check), because a screenshot can be stale. But a recording/demo run
// ALSO needs a trustworthy public-API viewport capture at each visual stage, plus the
// data that proves what the screenshot shows. This bundles both into one call
// and — crucially — GATES on the frame: if the capture is blank (window not
// rendering; see snapshot_frame.go) or stale (byte-identical to --previous),
// the explicit legacy duplicate gate can stop a recording script. Identical
// bytes alone do not prove an old capture when the design has not changed.
func newPcbStageSnapshotCmd(cfg *appConfig, window *string, stdout, stderr io.Writer) *cobra.Command {
	var (
		stage       string
		outDir      string
		noFit       bool
		fitMode     string
		previousSha string
		allowStale  bool
	)
	c := &cobra.Command{
		Use:   "stage-snapshot",
		Short: "Capture a recording/demo STAGE: board-fitted viewport PNG + data bundle, verified PNG, capture identity and hashed data",
		Long: `Capture one recording/demo STAGE in a single call:

  1. a public-API PCB viewport PNG  → <out>/<stage>/snapshot.png
  2. a data bundle (the proof)      → components/tracks/vias/pours/nets/drc .json
  3. a stage.json manifest with the frame verdict

Missing, corrupt, mismatched-hash or BLANK PNGs exit NON-ZERO. Each data
response records identity and timing; an observed document switch rejects the
bundle. --previous-sha256 is a legacy opt-in DUPLICATE-IMAGE gate, not proof of
capture age: unchanged designs may legitimately produce identical fresh PNGs.
--allow-stale downgrades that explicit duplicate gate to a warning.

No public API can force a hidden window to repaint. Use typed document switching
to activate the target PCB and re-run; if it remains BLANK, record the render as
unavailable instead of falling back to GUI operation. The default --fit-mode
board uses pcb_Document.zoomToBoardOutline before capture. This is not the editor
menu's object-level Copy-as-PNG/SVG export, which public eda.* does not expose.`,
		Args: cobra.NoArgs,
		Example: `  easyeda pcb stage-snapshot --project ceshi --stage "P7 routing"
  PREV=$(easyeda pcb stage-snapshot --stage P6 --out ./rec | jq -r .sha256)
  easyeda pcb stage-snapshot --stage P7 --out ./rec --previous-sha256 "$PREV"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(stage) == "" {
				return fmt.Errorf("--stage is required (e.g. --stage \"P7 routing\")")
			}

			mode, modeErr := normalizePcbSnapshotFitMode(fitMode)
			if modeErr != nil {
				return modeErr
			}
			if noFit {
				if cmd.Flags().Changed("fit-mode") {
					return fmt.Errorf("--no-fit and --fit-mode cannot be used together")
				}
				mode = "none"
			}
			return capturePcbStage(cfg, *window, stage, filepath.Join(outDirOrDefault(outDir), sanitizeStage(stage)), mode, previousSha, allowStale, stdout, stderr)
		},
	}
	c.Flags().StringVar(&stage, "stage", "", "stage label, e.g. \"P7 routing\" (required)")
	c.Flags().StringVar(&outDir, "out", "", "output root (default ./.easyeda/stages)")
	c.Flags().StringVar(&fitMode, "fit-mode", "board", "viewport fit: board | all | none (default board)")
	c.Flags().BoolVar(&noFit, "no-fit", false, "legacy alias for --fit-mode none")
	c.Flags().StringVar(&previousSha, "previous-sha256", "", "legacy opt-in duplicate-image gate; identical pixels alone do not prove stale capture")
	c.Flags().BoolVar(&allowStale, "allow-stale", false, "downgrade a stale (but non-blank) frame from error to warning")
	return c
}

func normalizePcbSnapshotFitMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "board", nil
	}
	switch mode {
	case "board", "all", "none":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --fit-mode %q: expected board, all, or none", mode)
	}
}

func resolveStageSnapshotSHA(reported, pngPath string) (sha, source string, err error) {
	if strings.TrimSpace(pngPath) == "" {
		return "", "unavailable", fmt.Errorf("snapshot PNG artifact is missing")
	}
	raw, err := os.ReadFile(pngPath)
	if err != nil {
		return "", "unavailable", err
	}
	actual := sha256Hex(raw)
	if strings.TrimSpace(reported) != "" && !strings.EqualFold(strings.TrimSpace(reported), actual) {
		return actual, "cli-artifact", fmt.Errorf("snapshot SHA256 mismatch: connector reports %s; persisted bytes are %s", reported, actual)
	}
	if strings.TrimSpace(reported) != "" {
		return actual, "connector+cli-artifact", nil
	}
	return actual, "cli-artifact", nil
}

// outDirOrDefault resolves the stage output root.
func outDirOrDefault(out string) string {
	if strings.TrimSpace(out) != "" {
		return out
	}
	return filepath.Join(".easyeda", "stages")
}

var stageSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitizeStage turns a human stage label into a safe directory name.
func sanitizeStage(stage string) string {
	s := stageSanitizeRe.ReplaceAllString(strings.TrimSpace(stage), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "stage"
	}
	return s
}

// drcPass extracts a pass/fail verdict from a pcb.drc.check result. The second
// return is false when the result shape does not expose a verdict we recognize.
func drcPass(res map[string]any) (passed bool, known bool) {
	if res == nil {
		return false, false
	}
	if v, ok := res["passed"].(bool); ok {
		return v, true
	}
	// Fall back to a zero violation count if "passed" is absent.
	for _, key := range []string{"count", "violationCount", "errorCount"} {
		if n, ok := res[key].(float64); ok {
			return n == 0, true
		}
	}
	return false, false
}

// writeJSONFile marshals v indented to path.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// copyFile copies src to dst (used to snapshot the artifact into the stage dir).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
