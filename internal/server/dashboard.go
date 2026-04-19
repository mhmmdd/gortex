package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/zzet/gortex/internal/config"
	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/indexer"
	"github.com/zzet/gortex/internal/server/hub"
)

// activityBuffer holds the last N graph-change events so the UI can
// backfill its activity feed without waiting for a fresh event.
//
// The buffer is intentionally small (default 100) — it is meant for
// "what just happened" feedback in the dashboard, not durable history.
// Events are preserved across reconnects but are lost on server restart.
type activityBuffer struct {
	mu     sync.RWMutex
	events []indexer.GraphChangeEvent
	cap    int
}

func newActivityBuffer(cap int) *activityBuffer {
	if cap <= 0 {
		cap = 100
	}
	return &activityBuffer{cap: cap, events: make([]indexer.GraphChangeEvent, 0, cap)}
}

func (b *activityBuffer) add(ev indexer.GraphChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev)
	if len(b.events) > b.cap {
		b.events = b.events[len(b.events)-b.cap:]
	}
}

func (b *activityBuffer) snapshot(limit int) []indexer.GraphChangeEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > len(b.events) {
		limit = len(b.events)
	}
	out := make([]indexer.GraphChangeEvent, limit)
	for i := 0; i < limit; i++ {
		out[i] = b.events[len(b.events)-1-i]
	}
	return out
}

func (h *Handler) startActivityCollector(eh *hub.Hub) {
	if eh == nil || h.activity == nil {
		return
	}
	subID, ch := eh.Subscribe()
	go func() {
		defer eh.Unsubscribe(subID)
		for ev := range ch {
			h.activity.add(ev)
		}
	}()
}

// --- /v1/activity ---

func (h *Handler) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if h.activity == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"events": h.activity.snapshot(limit)})
}

// --- /v1/repos ---
//
// Returns a flat list of indexed repositories with the per-repo
// breakdown the dashboard's repo cards and the graph filter panel
// expect. All numbers are derived from graph.RepoStats() — no mock
// values. The "color" hint is chosen deterministically from the repo
// prefix so cards stay stable across reloads without storing state.

type repoEntry struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Lang       string `json:"lang"`
	Nodes      int    `json:"nodes"`
	Edges      int    `json:"edges"`
	Funcs      int    `json:"funcs"`
	Methods    int    `json:"methods"`
	Types      int    `json:"types"`
	Interfaces int    `json:"interfaces"`
	Vars       int    `json:"vars"`
	Files      int    `json:"files"`
	Color      string `json:"color"`
}

// dominantLanguage returns the language with the highest byte/node share
// for a repo, ignoring config-only languages so a Go repo doesn't show
// up as "yaml".
func dominantLanguage(byLang map[string]int) string {
	skip := map[string]bool{"json": true, "yaml": true, "toml": true, "markdown": true, "makefile": true, "dockerfile": true, "bash": true, "hcl": true}
	best := ""
	bestN := -1
	for lang, n := range byLang {
		if skip[lang] {
			continue
		}
		if n > bestN {
			best = lang
			bestN = n
		}
	}
	if best != "" {
		return best
	}
	for lang, n := range byLang {
		if n > bestN {
			best = lang
			bestN = n
		}
	}
	return best
}

// hashColor maps a string to one of the design's accent OKLCH colors.
// Stable, deterministic, no per-repo seeding required.
var repoPalette = []string{
	"oklch(0.82 0.15 155)",
	"oklch(0.80 0.13 230)",
	"oklch(0.78 0.14 300)",
	"oklch(0.80 0.17 10)",
	"oklch(0.82 0.14 45)",
	"oklch(0.82 0.15 80)",
	"oklch(0.72 0.02 252)",
}

func paletteFor(s string) string {
	if s == "" {
		return repoPalette[0]
	}
	var sum uint32
	for _, c := range s {
		sum = sum*31 + uint32(c)
	}
	return repoPalette[int(sum)%len(repoPalette)]
}

// splitOwner pulls "owner/repo" out of a repo prefix when one exists,
// otherwise treats the whole prefix as the repo and leaves owner blank.
func splitOwner(prefix string) (owner, name string) {
	if i := strings.Index(prefix, "/"); i >= 0 {
		return prefix[:i], prefix[i+1:]
	}
	return "", prefix
}

func reposFromGraph(g *graph.Graph) []repoEntry {
	stats := g.RepoStats()
	out := make([]repoEntry, 0, len(stats))
	for prefix, s := range stats {
		owner, name := splitOwner(prefix)
		out = append(out, repoEntry{
			ID:         name,
			Owner:      owner,
			Lang:       dominantLanguage(s.ByLanguage),
			Nodes:      s.TotalNodes,
			Edges:      s.TotalEdges,
			Funcs:      s.ByKind["function"],
			Methods:    s.ByKind["method"],
			Types:      s.ByKind["type"],
			Interfaces: s.ByKind["interface"],
			Vars:       s.ByKind["variable"],
			Files:      s.ByKind["file"],
			Color:      paletteFor(prefix),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nodes > out[j].Nodes })
	return out
}

func (h *Handler) handleRepos(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"repos": reposFromGraph(h.graph)})
}

// --- /v1/processes ---
//
// Thin wrapper around the get_processes MCP tool. Returns processes in
// a UI-friendly shape: each process gets a "crosses" array (unique repo
// prefixes touched) and a "risk" rating derived from the score. When
// the underlying tool isn't registered (analyze-only build), returns
// an empty list rather than 404 so the page can render its empty state.

type processEntry struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Entry   string   `json:"entry"`
	Steps   int      `json:"steps"`
	Files   int      `json:"files"`
	Repos   int      `json:"repos"`
	Score   int      `json:"score"`
	Risk    string   `json:"risk"`    // ok | warn | risk
	Crosses []string `json:"crosses"` // repo prefixes this flow touches
}

func (h *Handler) handleProcesses(w http.ResponseWriter, r *http.Request) {
	raw := h.CallTool(r.Context(), "get_processes", map[string]any{})
	if raw == "" {
		WriteJSON(w, http.StatusOK, map[string]any{"processes": []processEntry{}})
		return
	}
	type rawProcess struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		EntryPoint string   `json:"entry_point"`
		Steps      []string `json:"steps"`
		StepCount  int      `json:"step_count"`
		Files      []string `json:"files"`
		FileCount  int      `json:"file_count"`
		Score      float64  `json:"score"`
	}
	type wrap struct {
		Processes []rawProcess `json:"processes"`
	}
	var w0 wrap
	if err := json.Unmarshal([]byte(raw), &w0); err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"processes": []processEntry{}})
		return
	}
	out := make([]processEntry, 0, len(w0.Processes))
	for _, p := range w0.Processes {
		crosses := uniqueRepoPrefixes(p.Steps)
		risk := "ok"
		switch {
		case p.Score > 1000:
			risk = "risk"
		case p.Score > 500:
			risk = "warn"
		}
		steps := p.StepCount
		if steps == 0 {
			steps = len(p.Steps)
		}
		files := p.FileCount
		if files == 0 {
			files = len(p.Files)
		}
		out = append(out, processEntry{
			ID:      p.ID,
			Name:    p.Name,
			Entry:   p.EntryPoint,
			Steps:   steps,
			Files:   files,
			Repos:   len(crosses),
			Score:   int(p.Score),
			Risk:    risk,
			Crosses: crosses,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	WriteJSON(w, http.StatusOK, map[string]any{"processes": out})
}

// uniqueRepoPrefixes derives the set of distinct "owner/repo" prefixes
// from a list of node IDs of the form "<repoPrefix>:<file>::<symbol>".
// Order is preserved (first occurrence wins) so the trace order in the
// UI mirrors the call sequence.
func uniqueRepoPrefixes(stepIDs []string) []string {
	seen := make(map[string]struct{}, len(stepIDs))
	out := make([]string, 0, 4)
	for _, id := range stepIDs {
		idx := strings.Index(id, ":")
		if idx < 0 {
			continue
		}
		repo := id[:idx]
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		out = append(out, repo)
	}
	return out
}

// --- /v1/contracts ---
//
// Flattens the contracts MCP tool's by_repo grouping into a single list
// keyed by canonical contract ID. The UI shows kind / producer /
// consumers / breaking flag; we fold provider+consumer rows into a
// single entry per contract ID so users see one row per route, not two.

type contractEntry struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"` // REST | EVENT | URL
	Producer  string   `json:"producer"`
	Consumers []string `json:"consumers"`
	Version   string   `json:"version"`
	Breaking  bool     `json:"breaking"`
	Callers   int      `json:"callers"`
	Last      string   `json:"last"`
}

func (h *Handler) handleContracts(w http.ResponseWriter, r *http.Request) {
	raw := h.CallTool(r.Context(), "contracts", map[string]any{"action": "list"})
	if raw == "" {
		WriteJSON(w, http.StatusOK, map[string]any{"contracts": []contractEntry{}})
		return
	}
	type rawContract struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Role       string         `json:"role"`
		SymbolID   string         `json:"symbol_id"`
		FilePath   string         `json:"file_path"`
		Line       int            `json:"line"`
		RepoPrefix string         `json:"repo_prefix"`
		Meta       map[string]any `json:"meta"`
		Confidence float64        `json:"confidence"`
	}
	type wrap struct {
		ByRepo map[string]struct {
			Contracts map[string][]rawContract `json:"contracts"`
			Total     int                      `json:"total"`
		} `json:"by_repo"`
	}
	var w0 wrap
	if err := json.Unmarshal([]byte(raw), &w0); err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"contracts": []contractEntry{}})
		return
	}
	merged := make(map[string]*contractEntry)
	for _, group := range w0.ByRepo {
		for kind, items := range group.Contracts {
			for _, c := range items {
				e, ok := merged[c.ID]
				if !ok {
					e = &contractEntry{
						ID:        c.ID,
						Name:      c.ID,
						Kind:      uiContractKind(kind),
						Consumers: []string{},
					}
					merged[c.ID] = e
				}
				if c.Role == "provider" && e.Producer == "" {
					e.Producer = c.RepoPrefix
				}
				if c.Role == "consumer" && c.RepoPrefix != "" && !contains(e.Consumers, c.RepoPrefix) {
					e.Consumers = append(e.Consumers, c.RepoPrefix)
				}
				e.Callers++
			}
		}
	}
	out := make([]contractEntry, 0, len(merged))
	for _, e := range merged {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Producer != out[j].Producer {
			return out[i].Producer < out[j].Producer
		}
		return out[i].Name < out[j].Name
	})
	WriteJSON(w, http.StatusOK, map[string]any{"contracts": out})
}

func uiContractKind(raw string) string {
	switch raw {
	case "topic":
		return "EVENT"
	case "ws":
		return "URL"
	case "http", "grpc", "graphql", "openapi":
		return "REST"
	case "env":
		return "URL"
	default:
		return strings.ToUpper(raw)
	}
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

// --- /v1/communities ---
//
// Returns the community detection result reshaped for the dashboard
// communities card. Each community gets symbol/file counts and a
// derived "growth" placeholder of "0%" — historical growth requires a
// time-series store the server doesn't yet maintain (see web/AGENTS.md).

type communityEntry struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Repo     string  `json:"repo"`
	Symbols  int     `json:"symbols"`
	Files    int     `json:"files"`
	Cohesion float64 `json:"cohesion"`
	Growth   string  `json:"growth"`
}

func (h *Handler) handleCommunities(w http.ResponseWriter, r *http.Request) {
	raw := h.CallTool(r.Context(), "get_communities", map[string]any{})
	if raw == "" {
		WriteJSON(w, http.StatusOK, map[string]any{"communities": []communityEntry{}, "modularity": 0.0})
		return
	}
	type rawComm struct {
		ID       string   `json:"id"`
		Label    string   `json:"label"`
		Members  []string `json:"members"`
		Files    []string `json:"files"`
		Size     int      `json:"size"`
		Cohesion float64  `json:"cohesion"`
	}
	type wrap struct {
		Communities []rawComm `json:"communities"`
		Modularity  float64   `json:"modularity"`
	}
	var w0 wrap
	if err := json.Unmarshal([]byte(raw), &w0); err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"communities": []communityEntry{}, "modularity": 0.0})
		return
	}
	out := make([]communityEntry, 0, len(w0.Communities))
	for _, c := range w0.Communities {
		// Repo prefix = prefix of first member; community labels can
		// span repos but the design only shows one badge so we pick the
		// majority prefix.
		repo := majorityRepoPrefix(c.Members)
		size := c.Size
		if size == 0 {
			size = len(c.Members)
		}
		out = append(out, communityEntry{
			ID:       c.ID,
			Name:     c.Label,
			Repo:     repo,
			Symbols:  size,
			Files:    len(c.Files),
			Cohesion: c.Cohesion,
			Growth:   "0%",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbols > out[j].Symbols })
	WriteJSON(w, http.StatusOK, map[string]any{
		"communities": out,
		"modularity":  w0.Modularity,
	})
}

func majorityRepoPrefix(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, id := range ids {
		i := strings.Index(id, ":")
		if i < 0 {
			continue
		}
		counts[id[:i]]++
	}
	best := ""
	bestN := -1
	for k, n := range counts {
		if n > bestN {
			best = k
			bestN = n
		}
	}
	return best
}

// --- /v1/guards ---
//
// Wraps check_guards into the table shape used by the Guards page. The
// MCP tool returns per-rule violations; we group by rule and report a
// status (ok | warn | violated) with the hit count.

type guardEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Hits   int    `json:"hits"`
	Scope  string `json:"scope"`
}

// handleGuards lists guard rules from configuration. Status is "ok" by
// default — rules don't have a runtime "violated" state until evaluated
// against a change set, which is the job of the check_guards MCP tool
// (callable via /v1/tools/check_guards with an `ids` argument). The
// page shows what's configured; the IDE / agent gets violations
// per-change.
func (h *Handler) handleGuards(w http.ResponseWriter, _ *http.Request) {
	out := make([]guardEntry, 0)
	seen := make(map[string]bool)
	add := func(rules []struct {
		Name, Kind, Source, Target string
	}) {
		for i, r := range rules {
			key := r.Name + "::" + r.Source + "::" + r.Target
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, guardEntry{
				ID:     fmt.Sprintf("%s-%d", r.Name, i),
				Name:   r.Name,
				Kind:   r.Kind,
				Status: "ok",
				Hits:   0,
				Scope:  r.Source + " → " + r.Target,
			})
		}
	}
	if h.configManager != nil {
		// Workspace overrides per active repo + global defaults at "".
		repos := append([]string{""}, repoNames(h.configManager.ActiveRepos())...)
		for _, name := range repos {
			rules := h.configManager.EffectiveGuardRules(name)
			compact := make([]struct {
				Name, Kind, Source, Target string
			}, 0, len(rules))
			for _, r := range rules {
				compact = append(compact, struct {
					Name, Kind, Source, Target string
				}{r.Name, r.Kind, r.Source, r.Target})
			}
			add(compact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	WriteJSON(w, http.StatusOK, map[string]any{"guards": out})
}

func repoNames(repos []config.RepoEntry) []string {
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// --- /v1/caveats ---

type caveatEntry struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // risk | hot | cycle | unowned | deprecated | boundary
	Symbol   string `json:"symbol"`
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	Owner    string `json:"owner"`
	Age      string `json:"age"`
}

func (h *Handler) handleCaveats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := make([]caveatEntry, 0, 32)

	// check_guards is intentionally NOT called here — the MCP tool
	// requires a `changed symbol IDs` argument and only returns
	// violations against that change set. The Caveats page is supposed
	// to surface persistent landmines, not what would fire if a
	// specific commit ran. Boundary/ownership violations come from the
	// /v1/guards endpoint instead.
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "hotspots", "limit": 20}); raw != "" {
		out = append(out, parseHotspots(raw)...)
	}
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "dead_code", "limit": 20}); raw != "" {
		out = append(out, parseDeadCode(raw)...)
	}
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "cycles", "limit": 20}); raw != "" {
		out = append(out, parseCycles(raw)...)
	}

	severityRank := map[string]int{
		"risk":       0,
		"hot":        1,
		"cycle":      2,
		"boundary":   3,
		"unowned":    4,
		"deprecated": 5,
	}
	sortByRank(out, severityRank)
	WriteJSON(w, http.StatusOK, map[string]any{"caveats": out})
}

func sortByRank(in []caveatEntry, rank map[string]int) {
	for i := 1; i < len(in); i++ {
		j := i
		for j > 0 && rank[in[j-1].Severity] > rank[in[j].Severity] {
			in[j-1], in[j] = in[j], in[j-1]
			j--
		}
	}
}

// All four parsers below produce IDs that combine a per-parser prefix
// with the entry index. The index is essential — the underlying tools
// can return entries with empty IDs (e.g. cycles, where the natural ID
// is the path), and the React UI uses the ID as a list key. Without
// the index suffix, multiple empty-ID entries collapse to the same key
// and React warns about duplicates.

func parseHotspots(raw string) []caveatEntry {
	// Mirrors analysis.HotspotEntry.
	type hotspot struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		FilePath string `json:"file_path"`
		Line     int    `json:"start_line"`
		FanIn    int    `json:"fan_in"`
	}
	type wrap struct {
		Hotspots []hotspot `json:"hotspots"`
	}
	var w wrap
	if err := json.Unmarshal([]byte(raw), &w); err != nil || len(w.Hotspots) == 0 {
		return nil
	}
	out := make([]caveatEntry, 0, len(w.Hotspots))
	for i, h := range w.Hotspots {
		if i >= 10 {
			break
		}
		name := h.Name
		if name == "" {
			name = h.ID
		}
		out = append(out, caveatEntry{
			ID:       fmt.Sprintf("hs-%d-%s", i, h.ID),
			Severity: "hot",
			Symbol:   h.ID,
			Title:    "Hot path · " + name,
			Desc:     fmt.Sprintf("Fan-in %d — touched by many call sites.", h.FanIn),
			Age:      "ongoing",
		})
	}
	return out
}

func parseDeadCode(raw string) []caveatEntry {
	// Mirrors analysis.DeadCodeEntry.
	type entry struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		FilePath string `json:"file_path"`
	}
	type wrap struct {
		DeadCode []entry `json:"dead_code"`
	}
	var w wrap
	if err := json.Unmarshal([]byte(raw), &w); err != nil || len(w.DeadCode) == 0 {
		return nil
	}
	out := make([]caveatEntry, 0, len(w.DeadCode))
	for i, e := range w.DeadCode {
		if i >= 10 {
			break
		}
		name := e.Name
		if name == "" {
			name = e.ID
		}
		out = append(out, caveatEntry{
			ID:       fmt.Sprintf("dc-%d-%s", i, e.ID),
			Severity: "deprecated",
			Symbol:   e.ID,
			Title:    "Likely unreachable · " + name,
			Desc:     "No incoming references in the indexed graph.",
		})
	}
	return out
}

func parseCycles(raw string) []caveatEntry {
	// Mirrors analysis.Cycle: { path[], kind, severity }. Earlier
	// versions of this parser looked for `id` and `members`, which the
	// real type doesn't have — every cycle ended up with an empty ID,
	// and the dashboard's React keys collided. Now the entry index is
	// part of the ID so duplicate or empty paths still render distinctly.
	type cycle struct {
		Path     []string `json:"path"`
		Kind     string   `json:"kind"`
		Severity int      `json:"severity"`
	}
	type wrap struct {
		Cycles []cycle `json:"cycles"`
	}
	var w wrap
	if err := json.Unmarshal([]byte(raw), &w); err != nil || len(w.Cycles) == 0 {
		return nil
	}
	out := make([]caveatEntry, 0, len(w.Cycles))
	for i, c := range w.Cycles {
		if i >= 10 {
			break
		}
		title := "Circular dependency"
		symbol := ""
		if len(c.Path) > 0 {
			symbol = c.Path[0]
			title = "Cycle: " + symbol
		}
		desc := fmt.Sprintf("%d symbols form a %s cycle.", len(c.Path), nonEmpty(c.Kind, "dependency"))
		out = append(out, caveatEntry{
			ID:       fmt.Sprintf("cy-%d-%s", i, symbol),
			Severity: "cycle",
			Symbol:   symbol,
			Title:    title,
			Desc:     desc,
		})
	}
	return out
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// --- /v1/dashboard ---
//
// Bundles every datum the dashboard hero card needs into one round-trip:
// totals, kinds + languages (as ordered arrays so the UI doesn't sort),
// repo cards, recent activity, and aggregated caveats. Designed to be
// cheap (one pass through stats + cached analyze tool results).

type dashboardSnapshot struct {
	Stats struct {
		TotalNodes int `json:"total_nodes"`
		TotalEdges int `json:"total_edges"`
		Repos      int `json:"repos"`
		Caveats    int `json:"caveats"`
		Version    string `json:"version"`
	} `json:"stats"`
	Kinds     []kvEntry                   `json:"kinds"`
	Languages []kvEntry                   `json:"languages"`
	Repos     []repoEntry                 `json:"repos"`
	Activity  []indexer.GraphChangeEvent  `json:"activity"`
	Caveats   []caveatEntry               `json:"caveats"`
	Processes []processEntry              `json:"processes"`
}

type kvEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func mapToOrderedKV(m map[string]int) []kvEntry {
	out := make([]kvEntry, 0, len(m))
	for k, v := range m {
		out = append(out, kvEntry{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats := h.graph.Stats()
	snap := dashboardSnapshot{}
	snap.Stats.TotalNodes = stats.TotalNodes
	snap.Stats.TotalEdges = stats.TotalEdges
	snap.Stats.Version = h.version
	snap.Kinds = mapToOrderedKV(stats.ByKind)
	snap.Languages = mapToOrderedKV(stats.ByLanguage)
	snap.Repos = reposFromGraph(h.graph)
	snap.Stats.Repos = len(snap.Repos)

	if h.activity != nil {
		snap.Activity = h.activity.snapshot(20)
	} else {
		snap.Activity = []indexer.GraphChangeEvent{}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Reuse the caveats aggregator so the count and the inline preview
	// both come from the same data — no chance of the dashboard's
	// number disagreeing with the Caveats page on first load.
	cavs := make([]caveatEntry, 0, 32)
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "hotspots", "limit": 20}); raw != "" {
		cavs = append(cavs, parseHotspots(raw)...)
	}
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "dead_code", "limit": 20}); raw != "" {
		cavs = append(cavs, parseDeadCode(raw)...)
	}
	if raw := h.CallTool(ctx, "analyze", map[string]any{"kind": "cycles", "limit": 20}); raw != "" {
		cavs = append(cavs, parseCycles(raw)...)
	}
	snap.Caveats = cavs
	snap.Stats.Caveats = len(cavs)

	// Top processes for the inline preview. The full list is on the
	// Processes page; here we cap at 6 so the dashboard stays compact.
	if raw := h.CallTool(ctx, "get_processes", map[string]any{}); raw != "" {
		type rawProcess struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			EntryPoint string   `json:"entry_point"`
			Steps      []string `json:"steps"`
			StepCount  int      `json:"step_count"`
			Files      []string `json:"files"`
			FileCount  int      `json:"file_count"`
			Score      float64  `json:"score"`
		}
		type wrap struct {
			Processes []rawProcess `json:"processes"`
		}
		var w0 wrap
		if json.Unmarshal([]byte(raw), &w0) == nil {
			procs := make([]processEntry, 0, len(w0.Processes))
			for _, p := range w0.Processes {
				crosses := uniqueRepoPrefixes(p.Steps)
				risk := "ok"
				switch {
				case p.Score > 1000:
					risk = "risk"
				case p.Score > 500:
					risk = "warn"
				}
				steps := p.StepCount
				if steps == 0 {
					steps = len(p.Steps)
				}
				files := p.FileCount
				if files == 0 {
					files = len(p.Files)
				}
				procs = append(procs, processEntry{
					ID:      p.ID,
					Name:    p.Name,
					Entry:   p.EntryPoint,
					Steps:   steps,
					Files:   files,
					Repos:   len(crosses),
					Score:   int(p.Score),
					Risk:    risk,
					Crosses: crosses,
				})
			}
			sort.Slice(procs, func(i, j int) bool { return procs[i].Score > procs[j].Score })
			if len(procs) > 6 {
				procs = procs[:6]
			}
			snap.Processes = procs
		}
	}
	if snap.Processes == nil {
		snap.Processes = []processEntry{}
	}

	WriteJSON(w, http.StatusOK, snap)
}
