// Package experiment embeds the offline evidence runner; no daemon or CAD actions.
package experiment

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
)

//go:embed runner.py
var runner string

func Run(args []string, stdout, stderr io.Writer) error {
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("experiment requires Python 3.9+: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(python, append([]string{"-c", runner}, args...)...)
	cmd.Env = append(os.Environ(), "EASYEDA_EXPERIMENT_CLI="+executable)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}
