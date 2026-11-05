// Package graph checks redundancy and structural consistency using a local code index.
package graph

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanmxa/norm/internal/axis"
	"github.com/yanmxa/norm/internal/codeindex"
	"github.com/yanmxa/norm/internal/target"
)

type Config struct {
	Thresholds  Thresholds
	MinSiblings int     // how many neighbours a convention needs before it is one
	MinShare    float64 // how consistent they must be
	MaxObs      int
}

func DefaultConfig() Config {
	return Config{
		Thresholds: DefaultThresholds(), MinSiblings: 5, MinShare: 0.8,
		MaxObs: 20,
	}
}

type Axis struct{ cfg Config }

func New(cfg Config) *Axis { return &Axis{cfg: cfg} }

func (a *Axis) ID() string { return "graph" }

func (a *Axis) Available(_ context.Context, t *target.Target) (bool, string) {
	if len(t.SupportedFiles()) == 0 {
		return false, "no supported source files changed"
	}
	return true, ""
}

func (a *Axis) Run(ctx context.Context, t *target.Target, prog chan<- axis.Progress) (*axis.Result, error) {
	r := &axis.Result{AxisID: a.ID()}

	send(prog, axis.Progress{AxisID: a.ID(), Stage: "index"})
	idx, err := codeindex.Build(ctx, t.Root)
	if err != nil {
		return nil, err
	}
	g := FromIndex(idx)
	r.Notes = append(r.Notes, "Native static call graph: dynamic and ambiguous calls are unresolved; new calls are compared by name only. Orphan and local convention checks are Go-only.")
	if idx.Unresolved > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d call sites unresolved (not evidence of absent dependencies)", idx.Unresolved))
	}

	iu, err := ScanIdents(t.Root)
	if err != nil {
		return nil, fmt.Errorf("scan identifiers: %w", err)
	}

	changed, newCallees, missing, baselineErr := a.changedNodes(ctx, t, g)
	if len(changed) == 0 {
		r.Measures = unmeasured("the index has none of the changed symbols — it may need rebuilding")
		return r, nil
	}
	if missing > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d changed functions are missing from the index", missing))
	}

	send(prog, axis.Progress{AxisID: a.ID(), Stage: "rules", Total: len(changed)})

	orphans := Orphans(g, changed, iu, iu.AppPackage)
	dups := append(NearDuplicates(g, changed, a.cfg.Thresholds),
		Reimplementations(g, changed, a.cfg.Thresholds)...)
	dups = append(dups, ExactBodyDuplicates(g, changed)...)
	divergence := SiblingDivergence(g, changed, a.cfg.MinSiblings, a.cfg.MinShare)

	// Bypassing and unprecedented dependencies are both questions about edges
	// the change drew. With no base revision every edge looks new, so they are
	// not asked at all rather than answered with noise.
	var bypassed, crossings []Finding
	if !t.WholeRepo && baselineErr == nil {
		bypassed = BypassedWrappers(g, changed, newCallees, a.cfg.Thresholds)
		crossings = LayerCrossings(g, changed, newCallees)
	}

	// Two readings, not five. Orphans and duplicates both mean the code did not
	// need to exist as written; the three consistency rules all mean it does not
	// fit what is already there. You act on each group the same way — read the
	// finding underneath — so five rows only made the table harder to scan.
	zero := 0.0
	count := func(key, label string, n int, note string) axis.Measure {
		m := axis.Measure{
			Key: key, Label: label, Value: float64(n), Unit: axis.UnitCount,
			RefHigh: &zero, Headline: true, Status: statusFor(n == 0),
		}
		if n > 0 {
			m.Note = note
		}
		return m
	}
	redundant := len(orphans) + len(dups)
	inconsistent := len(bypassed) + len(crossings) + len(divergence)

	inconsistentM := count("graph.inconsistent", "inconsistent code", inconsistent,
		breakdown("bypassed a wrapper", len(bypassed),
			"new dependency direction", len(crossings),
			"broke a local convention", len(divergence)))
	if t.WholeRepo {
		// Only one of the three rules could run. Reporting the partial count
		// under the same label would let a repository riddled with bypassed
		// wrappers read as clean.
		inconsistentM = axis.Measure{
			Key: "graph.inconsistent", Label: "inconsistent code", Headline: true,
			Status: axis.StatusUnmeasured,
			Note:   "needs a base revision; only convention drift was checked",
		}
	}

	if baselineErr != nil {
		inconsistentM.Status = axis.StatusUnmeasured
		inconsistentM.Note = "base comparison unavailable: " + baselineErr.Error()
	}

	r.Measures = []axis.Measure{
		count("graph.redundant", "redundant code", redundant,
			breakdown("unreachable", len(orphans), "duplicated", len(dups))),
		inconsistentM,
	}
	r.Diagnostics = map[string]float64{
		"graph.orphans":            float64(len(orphans)),
		"graph.duplicates":         float64(len(dups)),
		"graph.bypassed":           float64(len(bypassed)),
		"graph.layer_crossings":    float64(len(crossings)),
		"graph.sibling_divergence": float64(len(divergence)),
		"graph.changed_symbols":    float64(len(changed)),
		"graph.indexed_files":      float64(len(idx.Files)),
		"graph.resolved_calls":     float64(len(idx.Edges)),
		"graph.unresolved_calls":   float64(idx.Unresolved),
	}

	all := append(append(append(append(orphans, dups...), bypassed...), crossings...), divergence...)
	r.Observations = a.observations(all)
	return r, nil
}

// changedNodes maps the change's functions onto index symbols. A function the
// index does not know about is reported as a gap rather than silently dropped —
// a stale index would otherwise look like a clean bill of health.
func (a *Axis) changedNodes(ctx context.Context, t *target.Target, g *Graph) ([]Node, NewCallees, int, error) {
	var out []Node
	nc := NewCallees{}
	missing := 0
	var baselineErr error
	for _, cf := range t.SupportedFiles() {
		cur, err := parseChangedFile(ctx, t.Root, cf.Path)
		if err != nil {
			missing++
			baselineErr = err
			continue
		}
		if cur == nil {
			continue
		}
		baseCalls, err := baselineCalls(ctx, t, cf)
		if err != nil {
			baselineErr = err
		}
		curCalls := indexedCalls(*cur)
		for _, sym := range cur.Symbols {
			n, ok := g.Nodes[sym.ID]
			if !symbolTouched(sym, cf.Ranges) {
				continue
			}
			if !ok {
				missing++
				continue
			}
			out = append(out, n)
			nc.Add(n.Key(), Diff(curCalls[sym.Qualified], baseCalls[sym.Qualified]))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out, nc, missing, baselineErr
}

// parseChangedFile excludes tests, generated Go and non-regular files before parsing.
func parseChangedFile(ctx context.Context, root, path string) (*codeindex.File, error) {
	if target.IsTestFile(path) {
		return nil, nil
	}
	full := filepath.Join(root, path)
	info, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".go") {
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
		if err == nil && target.IsGenerated(f) {
			return nil, nil
		}
	}
	f, err := codeindex.Parse(ctx, path, src)
	return &f, err
}

func baselineCalls(ctx context.Context, t *target.Target, cf target.ChangedFile) (map[string]map[string]bool, error) {
	if t.WholeRepo || cf.Status == target.Added {
		return nil, nil
	}
	path := cf.Path
	if cf.Status == target.Renamed && cf.OldPath != "" {
		path = cf.OldPath
	}
	src, err := target.ShowFileStrict(ctx, t.Root, t.BaseSHA, path)
	if err != nil {
		return nil, err
	}
	base, err := codeindex.Parse(ctx, cf.Path, src)
	if err != nil {
		return nil, err
	}
	return indexedCalls(base), nil
}

func symbolTouched(s codeindex.Symbol, ranges []target.LineRange) bool {
	if s.Kind != "function" && s.Kind != "method" {
		return false
	}
	for _, r := range ranges {
		if r.Overlaps(s.StartLine, s.EndLine) {
			return true
		}
	}
	return false
}

func indexedCalls(f codeindex.File) map[string]map[string]bool {
	aliases := map[string]string{}
	for _, im := range f.Imports {
		if im.Name != "" && im.Name != "*" && im.Name != "default" {
			aliases[im.Alias] = im.Name
		}
	}
	out := map[string]map[string]bool{}
	for _, s := range f.Symbols {
		names := map[string]bool{}
		for _, c := range s.Calls {
			name := c.Name
			if c.Qualifier == "" && aliases[name] != "" {
				name = aliases[name]
			}
			names[name] = true
		}
		out[s.Qualified] = names
	}
	return out
}

func (a *Axis) observations(fs []Finding) []axis.Observation {
	if len(fs) > a.cfg.MaxObs {
		fs = fs[:a.cfg.MaxObs]
	}
	out := make([]axis.Observation, 0, len(fs))
	for _, f := range fs {
		out = append(out, axis.Observation{
			Path: f.Node.File, Line: f.Node.StartLine, EndLine: f.Node.EndLine,
			Kind: f.Rule, Title: f.Title, Detail: f.Detail,
		})
	}
	return out
}

// breakdown renders the non-zero parts of a merged reading, so the single
// number still says what it is made of.
func breakdown(pairs ...any) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		label, _ := pairs[i].(string)
		n, _ := pairs[i+1].(int)
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	return strings.Join(parts, ", ")
}

func unmeasured(why string) []axis.Measure {
	keys := []struct{ key, label string }{
		{"graph.redundant", "redundant code"},
		{"graph.inconsistent", "inconsistent code"},
	}
	var out []axis.Measure
	for _, k := range keys {
		out = append(out, axis.Measure{
			Key: k.key, Label: k.label, Headline: true,
			Status: axis.StatusUnmeasured, Note: why,
		})
	}
	return out
}

func statusFor(ok bool) axis.Status {
	if ok {
		return axis.StatusOK
	}
	return axis.StatusFail
}

func send(ch chan<- axis.Progress, p axis.Progress) {
	if ch == nil {
		return
	}
	select {
	case ch <- p:
	default:
	}
}
