package spec

import (
	"fmt"
	"strings"
	"testing"
)

func chainFixture() (ChainIntent, ChainGraph) {
	m := ChainIntent{SchemaVersion: 1}
	g := ChainGraph{SchemaVersion: 1, Complete: true, Coverage: []string{"board-copper", "module-internal", "joints"}, Sources: []GraphSource{{ID: "measured", Path: "synthetic.json", SHA256: strings.Repeat("a", 64)}}}
	for _, name := range []string{"A", "B"} {
		ns := []string{name + ".in", name + ".pad1", name + ".module1", name + ".module2", name + ".pad2", name + ".out"}
		for i, n := range ns {
			scope := "board"
			if i == 2 || i == 3 {
				scope = "module"
			}
			g.Nodes = append(g.Nodes, ChainNode{n, scope})
		}
		kinds := []string{"board-copper", "joint", "module-internal", "joint", "board-copper"}
		for i, k := range kinds {
			g.Edges = append(g.Edges, ChainEdge{ID: fmt.Sprintf("%s.e%d", name, i), From: ns[i], To: ns[i+1], Kind: k, Evidence: "measured"})
		}
		m.Chains = append(m.Chains, TestChain{ID: name, OrderedNodes: ns, RequiredJoints: []string{name + ".e1", name + ".e3"}})
	}
	return m, g
}
func TestChainDetectsElectricalFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		edit       func(*ChainIntent, *ChainGraph)
	}{
		{"good", "", func(m *ChainIntent, g *ChainGraph) {}},
		{"testpoint branch allowed", "", func(m *ChainIntent, g *ChainGraph) {
			g.Nodes = append(g.Nodes, ChainNode{"A.TP", "board"})
			g.Edges = append(g.Edges, ChainEdge{"tp", "A.pad1", "A.TP", "board-copper", "measured"})
		}},
		{"open", "open-chain", func(m *ChainIntent, g *ChainGraph) { g.Edges = append(g.Edges[:2], g.Edges[3:]...) }},
		{"shortcut", "joint-bypass", func(m *ChainIntent, g *ChainGraph) {
			g.Edges = append(g.Edges, ChainEdge{"shortcut", "A.pad1", "A.pad2", "board-copper", "measured"})
		}},
		{"parallel joint", "joint-bypass", func(m *ChainIntent, g *ChainGraph) { e := g.Edges[1]; e.ID = "parallel"; g.Edges = append(g.Edges, e) }},
		{"order", "node-order", func(m *ChainIntent, g *ChainGraph) { n := m.Chains[0].OrderedNodes; n[2], n[3] = n[3], n[2] }},
		{"cross chain", "cross-chain", func(m *ChainIntent, g *ChainGraph) {
			g.Edges = append(g.Edges, ChainEdge{"short", "A.pad1", "B.pad1", "board-copper", "measured"})
		}},
		{"unknown edge source", "invalid", func(m *ChainIntent, g *ChainGraph) { g.Edges[0].Evidence = "unknown" }},
		{"wrong physical scope", "invalid", func(m *ChainIntent, g *ChainGraph) { g.Edges[1].Kind = "board-copper" }},
		{"missing joint", "invalid", func(m *ChainIntent, g *ChainGraph) { m.Chains[0].RequiredJoints = []string{"unknown"} }},
		{"duplicate node", "invalid", func(m *ChainIntent, g *ChainGraph) { g.Nodes = append(g.Nodes, g.Nodes[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, g := chainFixture()
			tc.edit(&m, &g)
			r := CheckChains(m, g)
			if tc.code == "" {
				if r.Status != "pass" {
					t.Fatalf("%+v", r)
				}
				return
			}
			if r.Status != "fail" {
				t.Fatalf("unsafe graph accepted: %+v", r)
			}
			for _, i := range r.Issues {
				if i.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tc.code, r)
		})
	}
}
func TestChainMissingCoverageCannotPass(t *testing.T) {
	for _, edit := range []func(*ChainIntent, *ChainGraph){func(m *ChainIntent, g *ChainGraph) { g.Complete = false }, func(m *ChainIntent, g *ChainGraph) { g.Coverage = []string{"board-copper", "joints"} }, func(m *ChainIntent, g *ChainGraph) { g.Sources = nil; g.Edges = nil }, func(m *ChainIntent, g *ChainGraph) { m.Chains[0].RequiredJoints = nil }} {
		m, g := chainFixture()
		edit(&m, &g)
		if r := CheckChains(m, g); r.Status == "pass" {
			t.Fatal("incomplete evidence accepted")
		}
	}
}
