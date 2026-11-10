package app

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestKiCadArchive(t *testing.T) {
	for _, c := range []struct {
		name, body string
		ok         bool
	}{
		{"board.kicad_pcb", `(kicad_pcb (version 20260206) (property "x" "quoted \"()"))`, true},
		{"../board.kicad_pcb", `(kicad_pcb (version 1))`, false},
		{"board.brd", `(kicad_pcb (version 1))`, false},
		{"board.kicad_pcb", `(kicad_pcb`, false},
		{"board.kicad_pcb", `(kicad_pcb) ()`, false},
		{"board.kicad_pcb", `(kicad_pcbx (version 1))`, false},
	} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, _ := z.Create(c.name)
		w.Write([]byte(c.body))
		z.Close()
		e := validateKiCadArchive(b.Bytes())
		if (e == nil) != c.ok {
			t.Fatalf("%s %s: %v", c.name, c.body, e)
		}
	}
}
