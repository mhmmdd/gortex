package store_ladybug

import "github.com/zzet/gortex/internal/graph"

func (s *Store) NodeCount() int {
	rows := s.querySelect(`MATCH (n:Node) RETURN count(n)`, nil)
	if len(rows) == 0 {
		return 0
	}
	n, _ := rows[0][0].(int64)
	return int(n)
}

func (s *Store) EdgeCount() int {
	rows := s.querySelect(`MATCH ()-[e:Edge]->() RETURN count(e)`, nil)
	if len(rows) == 0 {
		return 0
	}
	n, _ := rows[0][0].(int64)
	return int(n)
}

func (s *Store) Stats() graph.GraphStats {
	st := graph.GraphStats{
		ByKind:     map[string]int{},
		ByLanguage: map[string]int{},
	}
	st.TotalNodes = s.NodeCount()
	st.TotalEdges = s.EdgeCount()

	rows := s.querySelect(`MATCH (n:Node) RETURN n.kind, count(n)`, nil)
	for _, r := range rows {
		kind, _ := r[0].(string)
		n, _ := r[1].(int64)
		if kind == "" {
			continue
		}
		st.ByKind[kind] = int(n)
	}
	rows = s.querySelect(`MATCH (n:Node) RETURN n.language, count(n)`, nil)
	for _, r := range rows {
		lang, _ := r[0].(string)
		n, _ := r[1].(int64)
		if lang == "" {
			continue
		}
		st.ByLanguage[lang] = int(n)
	}
	return st
}

func (s *Store) RepoStats() map[string]graph.GraphStats {
	out := map[string]graph.GraphStats{}
	rows := s.querySelect(`MATCH (n:Node) WHERE n.repo_prefix <> '' RETURN n.repo_prefix, n.kind, n.language, count(n)`, nil)
	for _, r := range rows {
		repo, _ := r[0].(string)
		kind, _ := r[1].(string)
		lang, _ := r[2].(string)
		n, _ := r[3].(int64)
		if repo == "" {
			continue
		}
		st, ok := out[repo]
		if !ok {
			st = graph.GraphStats{ByKind: map[string]int{}, ByLanguage: map[string]int{}}
		}
		st.TotalNodes += int(n)
		st.ByKind[kind] += int(n)
		st.ByLanguage[lang] += int(n)
		out[repo] = st
	}
	rows = s.querySelect(`
MATCH (a:Node)-[e:Edge]->(:Node)
WHERE a.repo_prefix <> ''
RETURN a.repo_prefix, count(e)`, nil)
	for _, r := range rows {
		repo, _ := r[0].(string)
		n, _ := r[1].(int64)
		if repo == "" {
			continue
		}
		st, ok := out[repo]
		if !ok {
			st = graph.GraphStats{ByKind: map[string]int{}, ByLanguage: map[string]int{}}
		}
		st.TotalEdges = int(n)
		out[repo] = st
	}
	return out
}

func (s *Store) RepoPrefixes() []string {
	rows := s.querySelect(`MATCH (n:Node) WHERE n.repo_prefix <> '' RETURN DISTINCT n.repo_prefix`, nil)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		p, _ := r[0].(string)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Store) EdgeIdentityRevisions() int {
	return int(s.edgeIdentityRevs.Load())
}

// VerifyEdgeIdentities is a no-op for the KuzuDB backend: there is a
// single canonical row per edge in the rel table, so the "same
// pointer in both adjacency views" invariant the in-memory store
// upholds is trivially satisfied here — no walk can find a
// divergence to report.
func (s *Store) VerifyEdgeIdentities() error { return nil }

const (
	perNodeByteEstimate = 256
	perEdgeByteEstimate = 128
)

func (s *Store) RepoMemoryEstimate(repoPrefix string) graph.RepoMemoryEstimate {
	var est graph.RepoMemoryEstimate
	rows := s.querySelect(`MATCH (n:Node) WHERE n.repo_prefix = $r RETURN count(n)`, map[string]any{"r": repoPrefix})
	if len(rows) == 0 {
		return est
	}
	n, _ := rows[0][0].(int64)
	rows = s.querySelect(`
MATCH (a:Node {repo_prefix: $r})-[e:Edge]->(:Node)
RETURN count(e)`, map[string]any{"r": repoPrefix})
	var e int64
	if len(rows) > 0 {
		e, _ = rows[0][0].(int64)
	}
	est.NodeCount = int(n)
	est.EdgeCount = int(e)
	est.NodeBytes = uint64(n) * perNodeByteEstimate
	est.EdgeBytes = uint64(e) * perEdgeByteEstimate
	return est
}

func (s *Store) AllRepoMemoryEstimates() map[string]graph.RepoMemoryEstimate {
	out := map[string]graph.RepoMemoryEstimate{}
	rows := s.querySelect(`MATCH (n:Node) WHERE n.repo_prefix <> '' RETURN n.repo_prefix, count(n)`, nil)
	for _, r := range rows {
		repo, _ := r[0].(string)
		n, _ := r[1].(int64)
		if repo == "" {
			continue
		}
		est := out[repo]
		est.NodeCount = int(n)
		est.NodeBytes = uint64(n) * perNodeByteEstimate
		out[repo] = est
	}
	rows = s.querySelect(`
MATCH (a:Node)-[e:Edge]->(:Node)
WHERE a.repo_prefix <> ''
RETURN a.repo_prefix, count(e)`, nil)
	for _, r := range rows {
		repo, _ := r[0].(string)
		n, _ := r[1].(int64)
		if repo == "" {
			continue
		}
		est := out[repo]
		est.EdgeCount = int(n)
		est.EdgeBytes = uint64(n) * perEdgeByteEstimate
		out[repo] = est
	}
	return out
}
