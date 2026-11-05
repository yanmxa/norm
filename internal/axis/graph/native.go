package graph

import (
	"fmt"
	"sort"

	"github.com/yanmxa/norm/internal/codeindex"
)

// FromIndex adapts the native, repository-local index to graph rule adjacency lists.
func FromIndex(idx *codeindex.Index) *Graph {
	g := &Graph{Nodes: map[string]Node{}, inbound: map[string][]Edge{}, outbound: map[string][]Edge{}, byFile: map[string][]string{}}
	for _, f := range idx.Files {
		for _, s := range f.Symbols {
			n := Node{ID: s.ID, Name: s.Name, Qualified: s.Qualified, Kind: s.Kind, File: f.Path, Language: f.Language, Signature: s.Signature, Fingerprint: s.Fingerprint, StartLine: s.StartLine, EndLine: s.EndLine}
			g.Nodes[n.ID] = n
			g.byFile[n.File] = append(g.byFile[n.File], n.ID)
		}
	}
	for _, e := range idx.Edges {
		edge := Edge{Source: e.Source, Target: e.Target, Kind: e.Kind, Line: e.Line}
		g.Edges = append(g.Edges, edge)
		g.inbound[e.Target] = append(g.inbound[e.Target], edge)
		g.outbound[e.Source] = append(g.outbound[e.Source], edge)
	}
	return g
}

// ExactBodyDuplicates compares nontrivial token fingerprints, not semantics.
// The parser leaves trivial bodies' fingerprints empty.
func ExactBodyDuplicates(g *Graph, changed []Node) []Finding {
	var out []Finding
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	for _, n := range changed {
		if n.IsGo() || !n.IsFunc() || n.Fingerprint == "" || isTestFile(n.File) {
			continue
		}
		for _, id := range ids {
			o := g.Nodes[id]
			if id == n.ID || o.File == n.File || !o.IsFunc() || o.Language != n.Language || o.Fingerprint != n.Fingerprint || isTestFile(o.File) {
				continue
			}
			key := pairKey(n.ID, id)
			if !seen[key] {
				seen[key] = true
				out = append(out, Finding{Rule: "exact-body-duplicate", Node: n, Other: &o, Title: fmt.Sprintf("%s and %s have identical body tokens", n.Label(), o.Label()), Detail: fmt.Sprintf("nontrivial body fingerprint matches %s:%d; this is syntactic duplication, not proof of semantic equivalence", o.File, o.StartLine)})
			}
			break
		}
	}
	return out
}
