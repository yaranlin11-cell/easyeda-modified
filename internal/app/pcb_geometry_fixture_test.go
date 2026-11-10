package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLayoutSquareCornerFixtureRejectsThreshold(t *testing.T) {
	var out, diagnostic bytes.Buffer
	window := ""
	cmd := newPcbLayoutScoreCmd(&appConfig{}, &window, &out, &diagnostic)
	cmd.SetArgs([]string{"--from", filepath.Join("testdata", "pcb-native-geometry", "square-corner.json"), "--only", "clearance", "--min-gap", "10", "--min-score", "99", "--json", "--all"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("square corner short passed numeric threshold: %s", out.String())
	}
	var response struct {
		Report layoutScoreReport `json:"report"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Report.Incomplete || response.Report.OK || len(response.Report.Blocking) == 0 {
		t.Fatalf("fixture did not isolate known copper defect: %+v", response.Report)
	}
}
