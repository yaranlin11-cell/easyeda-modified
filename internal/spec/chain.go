package spec

import "fmt"

// ChainIntent is independent of a measured graph; it must never be inferred from
// the same candidate being checked. Nodes refer to physical endpoints, not nets.
type ChainIntent struct {
	SchemaVersion int         `json:"schemaVersion"`
	Chains        []TestChain `json:"chains"`
}
type TestChain struct {
	ID             string   `json:"id"`
	OrderedNodes   []string `json:"orderedNodes"`
	RequiredJoints []string `json:"requiredJoints"`
}
type ChainNode struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}
type ChainEdge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Kind     string `json:"kind"`
	Evidence string `json:"evidence"`
}
type ChainGraph struct {
	SchemaVersion int           `json:"schemaVersion"`
	Complete      bool          `json:"complete"`
	Coverage      []string      `json:"coverage"`
	Sources       []GraphSource `json:"sources"`
	Nodes         []ChainNode   `json:"nodes"`
	Edges         []ChainEdge   `json:"edges"`
}
type GraphSource struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type chainArc struct {
	to   string
	edge string
}

// CheckChains checks only an explicitly supplied undirected continuity graph.
// It proves neither the graph extraction nor resistance, impedance or hardware.
func CheckChains(intent ChainIntent, graph ChainGraph) EvidenceReport {
	r := EvidenceReport{Status: "pass", Scope: "offline-supplied-physical-graph", Issues: []EvidenceIssue{}, Limitations: []string{"graph completeness and edge evidence are caller declarations; source binding is checked separately", "no automatic PCB extraction, live persistence, electrical resistance or manufacturing proof", "all declared chains must be electrically isolated from each other"}}
	fail := func(id, code, detail string) { r.Status = "fail"; r.issue(code, id, detail) }
	if intent.SchemaVersion != 1 || graph.SchemaVersion != 1 {
		fail("schemaVersion", "invalid", "expected schemaVersion 1")
	}
	if !graph.Complete {
		if r.Status != "fail" {
			r.Status = "incomplete"
		}
		r.issue("incomplete", "graph", "complete physical graph not available")
	}
	cover := map[string]bool{}
	for _, s := range graph.Coverage {
		if cover[s] || !oneOf(s, "board-copper", "module-internal", "joints") {
			fail(s, "invalid", "invalid/duplicate coverage")
		}
		cover[s] = true
	}
	for _, s := range []string{"board-copper", "module-internal", "joints"} {
		if !cover[s] {
			if r.Status != "fail" {
				r.Status = "incomplete"
			}
			r.issue("incomplete", s, "coverage not declared")
		}
	}
	sourceIDs := map[string]bool{}
	for _, s := range graph.Sources {
		if s.ID == "" || sourceIDs[s.ID] || s.Path == "" || len(s.SHA256) != 64 {
			fail(s.ID, "invalid", "unique bound source required")
		}
		sourceIDs[s.ID] = true
	}
	if len(graph.Sources) == 0 {
		if r.Status != "fail" {
			r.Status = "incomplete"
		}
		r.issue("incomplete", "sources", "graph source binding missing")
	}
	nodes := map[string]ChainNode{}
	adj := map[string][]chainArc{}
	for _, n := range graph.Nodes {
		if _, ok := nodes[n.ID]; ok || n.ID == "" || !oneOf(n.Scope, "board", "module") {
			fail(n.ID, "invalid", "unique physical node and board/module scope required")
		}
		nodes[n.ID] = n
	}
	edges := map[string]ChainEdge{}
	for _, e := range graph.Edges {
		if _, ok := edges[e.ID]; ok || e.ID == "" {
			fail(e.ID, "invalid", "empty/duplicate edge ID")
		}
		edges[e.ID] = e
		a, aok := nodes[e.From]
		b, bok := nodes[e.To]
		if !aok || !bok || e.From == e.To {
			fail(e.ID, "invalid", "invalid edge endpoints")
		}
		if !oneOf(e.Kind, "board-copper", "module-internal", "joint") {
			fail(e.ID, "invalid", "explicit edge kind required")
		}
		if e.Kind == "board-copper" && (a.Scope != "board" || b.Scope != "board") || e.Kind == "module-internal" && (a.Scope != "module" || b.Scope != "module") || e.Kind == "joint" && a.Scope == b.Scope {
			fail(e.ID, "invalid", "edge kind disagrees with endpoint scopes")
		}
		if !sourceIDs[e.Evidence] {
			fail(e.ID, "invalid", "edge must reference a bound source ID")
		}
		adj[e.From] = append(adj[e.From], chainArc{e.To, e.ID})
		adj[e.To] = append(adj[e.To], chainArc{e.From, e.ID})
	}
	chainIDs := map[string]bool{}
	for _, c := range intent.Chains {
		if c.ID == "" || chainIDs[c.ID] || len(c.OrderedNodes) < 2 {
			fail(c.ID, "invalid", "unique chain and at least two ordered nodes required")
		}
		chainIDs[c.ID] = true
		seen := map[string]bool{}
		for _, n := range c.OrderedNodes {
			if _, ok := nodes[n]; !ok || seen[n] {
				fail(c.ID, "invalid", "unknown/duplicate ordered node "+n)
			}
			seen[n] = true
		}
		seen = map[string]bool{}
		for _, id := range c.RequiredJoints {
			e, ok := edges[id]
			if !ok || e.Kind != "joint" || seen[id] {
				fail(c.ID, "invalid", "required joint missing, duplicate or not a joint: "+id)
			}
			seen[id] = true
		}
		if len(c.RequiredJoints) == 0 {
			if r.Status != "fail" {
				r.Status = "incomplete"
			}
			r.issue("incomplete", c.ID, "no required test joints declared")
		}
	}
	if len(intent.Chains) == 0 {
		fail("chains", "invalid", "at least one chain required")
	}
	// Do not produce topology PASS from incomplete or malformed evidence.
	if r.Status != "pass" {
		return r
	}
	path := func(start, end, skipNode, skipEdge string) []string {
		if start == skipNode || end == skipNode {
			return nil
		}
		queue := []string{start}
		prev := map[string]string{start: ""}
		found := false
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			if u == end {
				found = true
				break
			}
			for _, a := range adj[u] {
				if a.to == skipNode || a.edge == skipEdge {
					continue
				}
				if _, ok := prev[a.to]; !ok {
					prev[a.to] = u
					queue = append(queue, a.to)
				}
			}
		}
		if !found {
			return nil
		}
		out := []string{}
		for u := end; u != ""; u = prev[u] {
			out = append(out, u)
		}
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
	for _, c := range intent.Chains {
		start, end := c.OrderedNodes[0], c.OrderedNodes[len(c.OrderedNodes)-1]
		route := path(start, end, "", "")
		if route == nil {
			fail(c.ID, "open-chain", "entry and exit are disconnected")
			continue
		}
		index := map[string]int{}
		for i, n := range route {
			index[n] = i
		}
		last := -1
		for _, n := range c.OrderedNodes {
			at, ok := index[n]
			if !ok || at <= last {
				fail(c.ID, "node-order", "required nodes are absent or out of order on path")
			}
			last = at
			if n != start && n != end && path(start, end, n, "") != nil {
				fail(c.ID, "node-bypass", "entry/exit remain connected without required node "+n)
			}
		}
		for _, id := range c.RequiredJoints {
			if path(start, end, "", id) != nil {
				fail(c.ID, "joint-bypass", "entry/exit remain connected after opening required joint "+id)
			}
		}
	}
	for i, a := range intent.Chains {
		for _, b := range intent.Chains[i+1:] {
			// Check every required node, including disconnected fragments of a broken chain.
			joined := false
			for _, x := range a.OrderedNodes {
				for _, y := range b.OrderedNodes {
					if path(x, y, "", "") != nil {
						joined = true
						break
					}
				}
				if joined {
					break
				}
			}
			if joined {
				fail(a.ID, "cross-chain", fmt.Sprintf("electrically connected to chain %s", b.ID))
			}
		}
	}
	return r
}
