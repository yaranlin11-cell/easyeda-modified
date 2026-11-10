package app

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/zhoushoujianwork/easyeda-agent/internal/experiment"
)

func newExperimentCmd(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{Use: "experiment", Short: "Offline experiment records, checks and evidence-based diagnosis", Long: "Freeze inputs and append candidate-bound evidence. No EDA writes, arbitrary commands, automatic repair or full-design acceptance. Requires Python 3.9+."}
	specs := []struct {
		name, description  string
		required, optional []string
	}{
		{"export-copper", "Export native straight FILL geometry as diagnostic Gerber", []string{"from", "out", "repo"}, nil},
		{"extract", "Extract partial track endpoint graph (never full connectivity)", []string{"from", "out", "repo"}, nil},
		{"init", "Freeze baseline into a NEW directory", []string{"from", "out"}, nil},
		{"record", "Append a candidate and evidence; never overwrite a trial", []string{"run", "from"}, nil},
		{"check", "Run one registered offline checker and retain raw results", []string{"run", "trial", "checker", "inputs"}, []string{"repo"}},
		{"verify", "Verify evidence integrity and required offline check coverage", []string{"run"}, nil},
		{"diagnose", "Explain observed failures without inferring an unproven root cause", []string{"run", "trial"}, nil},
	}
	examples := map[string]string{
		"export-copper": "--from native-export.json --out diagnostic.zip --repo .",
		"extract":       "--from reference.kicad_pcb --out graph.json --repo .",
		"init":          "--from baseline.json --out new-run",
		"record":        "--run new-run --from trial.json",
		"check":         "--run new-run --trial x1-y1 --checker native-vias --inputs inputs.json --repo .",
		"verify":        "--run new-run",
		"diagnose":      "--run new-run --trial x1-y1",
	}
	for _, s := range specs {
		s := s
		c := &cobra.Command{Use: s.name, Short: s.description, Args: cobra.NoArgs, Example: "  easyeda experiment " + s.name + " " + examples[s.name]}
		for _, key := range append(append([]string{}, s.required...), s.optional...) {
			c.Flags().String(key, "", "experiment "+key+" (see experiment-records reference)")
		}
		for _, key := range s.required {
			_ = c.MarkFlagRequired(key)
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			argv := []string{s.name}
			for _, key := range append(append([]string{}, s.required...), s.optional...) {
				value, _ := cmd.Flags().GetString(key)
				if value != "" {
					argv = append(argv, "--"+key, value)
				}
			}
			return experiment.Run(argv, out, errOut)
		}
		root.AddCommand(c)
	}
	return root
}
