package experiment

import (
	_ "embed"
	"os/exec"
	"testing"
)

// Embed test source so changes invalidate the Go test cache.
//
//go:embed test_runner.py
var pythonTestSource string

func TestEvidenceRunner(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3.9+ required for experiment tests")
	}
	cmd := exec.Command(python, "-m", "unittest", "discover", "-s", ".", "-p", "test_*.py")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
