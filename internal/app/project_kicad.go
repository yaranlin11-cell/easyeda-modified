package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// One-board ZIP transport only: no extraction, secondary projects or libraries.
func validateKiCadArchive(data []byte) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(z.File) != 1 {
		return fmt.Errorf("KiCad ZIP must contain exactly one root .kicad_pcb file")
	}
	f := z.File[0]
	if strings.ContainsAny(f.Name, "/\\") || !strings.HasSuffix(strings.ToLower(f.Name), ".kicad_pcb") || f.UncompressedSize64 > 128<<20 {
		return fmt.Errorf("invalid KiCad entry path/type/size")
	}
	rd, err := f.Open()
	if err != nil {
		return err
	}
	defer rd.Close()
	b, err := io.ReadAll(io.LimitReader(rd, (128<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 128<<20 {
		return fmt.Errorf("KiCad board exceeds 128 MiB")
	}
	return validateKiCadBoard(b)
}
func validateKiCadBoard(b []byte) error {
	s := strings.TrimSpace(string(b))
	if !utf8.ValidString(s) || !strings.HasPrefix(s, "(kicad_pcb") || len(s) < 11 || !strings.ContainsAny(s[10:11], " \r\n\t") {
		return fmt.Errorf("expected KiCad PCB S-expression")
	}
	depth := 0
	quoted, escaped := false, false
	for i, c := range s {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == '(' {
			depth++
		}
		if c == ')' {
			depth--
		}
		if depth < 0 || depth > 1024 || (depth == 0 && i != len(s)-1) {
			return fmt.Errorf("invalid KiCad root/nesting")
		}
	}
	if quoted || depth != 0 {
		return fmt.Errorf("truncated KiCad board")
	}
	return nil
}
