package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceCLIUsesRelativeSourceHashes(t *testing.T) {
	dir := t.TempDir()
	src := []byte("synthetic reference")
	sum := sha256.Sum256(src)
	h := hex.EncodeToString(sum[:])
	os.WriteFile(filepath.Join(dir, "source.txt"), src, 0600)
	m := map[string]any{"schemaVersion": 1, "mode": "reference-assisted", "scopes": []string{"carrier"}, "sources": []any{map[string]any{"id": "sch", "path": "source.txt", "sha256": h, "role": "schematic-reference", "scope": "carrier", "revision": "1", "readStatus": "partial"}}, "requirements": []any{}, "capabilities": []any{}, "unresolved": []any{}}
	raw, _ := json.Marshal(m)
	file := filepath.Join(dir, "manifest.json")
	os.WriteFile(file, raw, 0600)
	var out, err bytes.Buffer
	if Run([]string{"spec", "reference-check", "--from", file}, &out, &err) == 0 {
		t.Fatal("partial reference accepted")
	}
	if !strings.Contains(out.String(), `"status": "incomplete"`) {
		t.Fatalf("%s %s", &out, &err)
	}
	out.Reset()
	err.Reset()
	os.WriteFile(filepath.Join(dir, "source.txt"), []byte("changed"), 0600)
	if Run([]string{"spec", "reference-check", "--from", file}, &out, &err) == 0 || !strings.Contains(out.String(), "SHA256 mismatch") {
		t.Fatal("changed source accepted", out.String(), err.String())
	}
}
func TestChainCLIRejectsUnboundSource(t *testing.T) {
	dir := t.TempDir()
	intent := `{"schemaVersion":1,"chains":[{"id":"chain","orderedNodes":["IN","M","OUT"],"requiredJoints":["j1","j2"]}]}`
	graph := `{"schemaVersion":1,"complete":true,"coverage":["board-copper","module-internal","joints"],"sources":[{"id":"src","path":"missing.txt","sha256":"` + strings.Repeat("a", 64) + `"}],"nodes":[{"id":"IN","scope":"board"},{"id":"M","scope":"module"},{"id":"OUT","scope":"board"}],"edges":[{"id":"j1","from":"IN","to":"M","kind":"joint","evidence":"src"},{"id":"j2","from":"M","to":"OUT","kind":"joint","evidence":"src"}]}`
	a := filepath.Join(dir, "intent.json")
	b := filepath.Join(dir, "graph.json")
	os.WriteFile(a, []byte(intent), 0600)
	os.WriteFile(b, []byte(graph), 0600)
	var out, err bytes.Buffer
	if Run([]string{"spec", "chain-check", "--from", a, "--observed", b}, &out, &err) == 0 || !strings.Contains(out.String(), "source-binding") {
		t.Fatalf("unbound graph accepted: %s %s", &out, &err)
	}
	source := []byte("synthetic graph source")
	sum := sha256.Sum256(source)
	if e := os.WriteFile(filepath.Join(dir, "missing.txt"), source, 0600); e != nil {
		t.Fatal(e)
	}
	graph = strings.Replace(graph, strings.Repeat("a", 64), hex.EncodeToString(sum[:]), 1)
	if e := os.WriteFile(b, []byte(graph), 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	err.Reset()
	if Run([]string{"spec", "chain-check", "--from", a, "--observed", b}, &out, &err) != 0 || !strings.Contains(out.String(), `"status": "pass"`) {
		t.Fatalf("valid bound graph rejected: %s %s", &out, &err)
	}
	if e := os.WriteFile(filepath.Join(dir, "missing.txt"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	err.Reset()
	if Run([]string{"spec", "chain-check", "--from", a, "--observed", b}, &out, &err) == 0 || !strings.Contains(out.String(), "source-binding") {
		t.Fatalf("changed source accepted: %s %s", &out, &err)
	}
}
func TestSourceProbeCLIIsNotExtractionSuccess(t *testing.T) {
	p := filepath.Join(t.TempDir(), "unknown.brd")
	os.WriteFile(p, []byte("arbitrary binary bytes"), 0600)
	var out, err bytes.Buffer
	if Run([]string{"spec", "source-probe", p}, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if !strings.Contains(out.String(), `"extractionStatus": "unreadable"`) {
		t.Fatal(out.String())
	}
}

func TestConstraintsCLIDoesNotAcceptAbsentMeasurements(t *testing.T) {
	dir := t.TempDir()
	source := []byte("synthetic minimum requirement")
	sum := sha256.Sum256(source)
	if e := os.WriteFile(filepath.Join(dir, "spec.txt"), source, 0600); e != nil {
		t.Fatal(e)
	}
	want := `{"schemaVersion":1,"sources":[{"id":"s","path":"spec.txt","sha256":"` + hex.EncodeToString(sum[:]) + `"}],"rules":[{"id":"hole","scope":"carrier","subject":"minimum-via-hole","metric":"diameter","unit":"um","stage":"design","mode":"min","min":200,"evidence":{"sourceId":"s","locator":"line 1"}}]}`
	a, b := filepath.Join(dir, "rules.json"), filepath.Join(dir, "observed.json")
	for path, body := range map[string]string{a: want, b: `{"schemaVersion":1,"sources":[],"measurements":[]}`} {
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	var out, errs bytes.Buffer
	if Run([]string{"spec", "constraints-check", "--from", a, "--observed", b}, &out, &errs) == 0 || !strings.Contains(out.String(), `"status": "incomplete"`) {
		t.Fatalf("%s %s", &out, &errs)
	}
}
