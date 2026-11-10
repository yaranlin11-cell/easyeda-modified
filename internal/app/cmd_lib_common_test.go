package app

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestLibraryCommonListDispatchesSeparateCatalogueAction(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]any
	}{
		{"all", []string{"common", "list"}, map[string]any{}},
		{"model", []string{"common", "list", "--category", "安装器件", "--group", "螺丝", "--query", "M3"}, map[string]any{"category": "安装器件", "group": "螺丝", "query": "M3"}},
		{"ungrouped", []string{"common", "list", "--group", ""}, map[string]any{"group": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := executeExamCommand(t, tc.args, func(cfg *appConfig, stdout, stderr *bytes.Buffer) commandExecutor {
				return newLibCmd(cfg, stdout, stderr)
			})
			captured.mu.Lock()
			defer captured.mu.Unlock()
			if captured.action != "library.common.list" || !reflect.DeepEqual(captured.payload, tc.want) {
				t.Fatalf("action=%s payload=%#v, want catalogue action and %#v", captured.action, captured.payload, tc.want)
			}
		})
	}
}

func TestLibraryCommonListHelpDescribesIdentityBoundary(t *testing.T) {
	var out bytes.Buffer
	cmd := newLibCmd(nil, &out, &out)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"common", "list", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"常用库", "--category", "--group", "--query", "lib device get", "Personal", "selected model"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q", want)
		}
	}
}

func TestLibSearchAcceptsPositionalQuery(t *testing.T) {
	build := func(cfg *appConfig, stdout, stderr *bytes.Buffer) commandExecutor { return newLibCmd(cfg, stdout, stderr) }
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"search", "C25804"}, "C25804"},
		{[]string{"search", "100nF", "0402"}, "100nF 0402"},
		{[]string{"search", "--query", "ESP32-S3-WROOM-1"}, "ESP32-S3-WROOM-1"},
	} {
		captured := executeExamCommand(t, tc.args, build)
		captured.mu.Lock()
		got := captured.payload["query"]
		captured.mu.Unlock()
		if captured.action != "schematic.library.search" || got != tc.want {
			t.Fatalf("%v: action=%s query=%v, want %q", tc.args, captured.action, got, tc.want)
		}
	}
	for _, args := range [][]string{{"search"}, {"search", "C1", "--query", "C2"}} {
		var stdout, stderr bytes.Buffer
		cmd := newLibCmd(&appConfig{}, &stdout, &stderr)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "query") {
			t.Fatalf("%v: want a query error, got %v", args, err)
		}
	}
}
