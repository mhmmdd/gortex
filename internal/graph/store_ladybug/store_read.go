package store_ladybug

import (
	"iter"
	"strings"

	"github.com/zzet/gortex/internal/graph"
)

// GetNode returns the node with the given id, or nil if absent.
//
// Uses the WHERE form on the PK to match the rest of the read
// surface (GetInEdges, FindNodesByName, GetFileSubGraph etc.) —
// the inline `{id: $id}` shape has been observed to return empty
// under concurrent writers when the planner picks a plan that
// doesn't survive a buffer-pool refresh.
func (s *Store) GetNode(id string) *graph.Node {
	const q = `MATCH (n:Node) WHERE n.id = $id RETURN ` + nodeReturnCols + ` LIMIT 1`
	rows := s.querySelect(q, map[string]any{"id": id})
	if len(rows) == 0 {
		return nil
	}
	return rowToNode(rows[0])
}

// GetNodeByQualName returns the first node whose qual_name matches,
// or nil if absent / empty.
func (s *Store) GetNodeByQualName(qualName string) *graph.Node {
	if qualName == "" {
		return nil
	}
	const q = `MATCH (n:Node) WHERE n.qual_name = $q RETURN ` + nodeReturnCols + ` LIMIT 1`
	rows := s.querySelect(q, map[string]any{"q": qualName})
	if len(rows) == 0 {
		return nil
	}
	return rowToNode(rows[0])
}

// FindNodesByName returns every node whose Name matches.
//
// The predicate is expressed as an outer `WHERE n.name = $name`
// instead of an inline `(n:Node {name: $name})`. Same shape as the
// GetInEdges fix elsewhere in this file: the inline-property form on
// a non-PK column has been observed to return empty rows under
// concurrent writers (the planner picks a plan that doesn't survive
// a buffer-pool refresh), while the WHERE form goes through the
// straightforward filter scan and stays correct. Both forms hit the
// same name index on Kuzu's side, so there is no measurable cost
// difference — only the correctness gap.
//
// This is the inbound-lookup the resolver's resolveMethodCall path
// uses via FindNodesByNameInRepo; an empty result there leaves the
// caller→method edge as `unresolved::Foo`, which is why
// `find_usages` on `Graph.AddNode` returned zero callers despite
// dozens of `g.AddNode(...)` call sites.
func (s *Store) FindNodesByName(name string) []*graph.Node {
	// Note: an earlier revision routed this through s.nameIdx with a
	// lazy bootstrap that ran a full Cypher scan. Under the parallel
	// warmup's per-repo IndexCtx pressure, the bootstrap Cypher
	// running concurrently with other Cypher writers tickled a
	// liblbug-side semasleep panic that crashed the daemon
	// mid-warmup. Keeping FindNodesByName on the engine path
	// preserves the correctness contract — the resolver's per-edge
	// lookup still hits Kuzu's secondary name index — and SearchSymbols
	// continues to consult s.nameIdx directly via lookupNodes for its
	// tier-0 fast path.
	const q = `MATCH (n:Node) WHERE n.name = $name RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"name": name})
	return rowsToNodes(rows)
}

// FindNodesByNameInRepo restricts FindNodesByName to one repo prefix.
// Same WHERE-clause rationale as FindNodesByName above — the inline
// two-property `{name: ..., repo_prefix: ...}` form was the resolver's
// primary call-edge lookup and the most likely culprit behind
// "method has obvious callers in source but find_usages returns 0".
func (s *Store) FindNodesByNameInRepo(name, repoPrefix string) []*graph.Node {
	const q = `MATCH (n:Node) WHERE n.name = $name AND n.repo_prefix = $repo RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"name": name, "repo": repoPrefix})
	return rowsToNodes(rows)
}

// FindNodesByNameContaining pushes the case-insensitive substring
// filter into a single Cypher MATCH so only matching rows cross the
// cgo boundary. Replaces the pre-existing search-substring fallback
// pattern of AllNodes()-then-filter (which materialised the entire
// node table per call — 68k rows for gortex's own graph; orders of
// magnitude more on Linux-kernel-sized indexes).
//
// Ladybug's CONTAINS is not backed by an index here, so the cost is
// still a server-side scan — but the row count crossing cgo is bound
// to the matching subset rather than every node in the graph, and the
// scan happens inside the engine's hot path rather than over a Go
// for-loop. limit caps the result; 0 means "no limit".
func (s *Store) FindNodesByNameContaining(substr string, limit int) []*graph.Node {
	if substr == "" {
		return nil
	}
	// LOWER(...) on both sides keeps the match case-insensitive; the
	// graph treats `Login` / `login` as distinct names but a substring
	// fallback wants to surface both. ToLower in Go before the bind so
	// the engine never has to call LOWER on the literal.
	needle := strings.ToLower(substr)
	if limit > 0 {
		const q = `MATCH (n:Node) WHERE LOWER(n.name) CONTAINS $q RETURN ` + nodeReturnCols + ` LIMIT $k`
		rows := s.querySelect(q, map[string]any{"q": needle, "k": int64(limit)})
		return rowsToNodes(rows)
	}
	const q = `MATCH (n:Node) WHERE LOWER(n.name) CONTAINS $q RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"q": needle})
	return rowsToNodes(rows)
}

// GetFileNodes returns every node anchored to filePath.
func (s *Store) GetFileNodes(filePath string) []*graph.Node {
	// Fast path via the Go-side file→id accelerator: hand the ids
	// straight to a primary-key MATCH so Kuzu uses the HASH PK
	// index instead of full-scanning Node to find a missing
	// file_path secondary index.
	if s.fileIDs != nil {
		ids := s.fileIDs.idsFor(filePath)
		if len(ids) == 0 {
			return nil
		}
		const q = `MATCH (n:Node) WHERE n.id IN $ids RETURN ` + nodeReturnCols
		rows := s.querySelect(q, map[string]any{"ids": stringSliceToAny(ids)})
		return rowsToNodes(rows)
	}
	const q = `MATCH (n:Node) WHERE n.file_path = $f RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"f": filePath})
	return rowsToNodes(rows)
}

// GetRepoNodes returns every node in the given repo prefix.
func (s *Store) GetRepoNodes(repoPrefix string) []*graph.Node {
	const q = `MATCH (n:Node) WHERE n.repo_prefix = $r RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"r": repoPrefix})
	return rowsToNodes(rows)
}

// GetOutEdges returns every edge whose From matches nodeID. Uses
// WHERE-form on the PK to match the GetInEdges / GetNode contract —
// the inline `{id: $id}` shape has been observed to return empty
// rows under concurrent writers.
func (s *Store) GetOutEdges(nodeID string) []*graph.Edge {
	const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE a.id = $id RETURN ` + edgeReturnCols
	rows := s.querySelect(q, map[string]any{"id": nodeID})
	return rowsToEdges(rows)
}

// GetRepoEdges returns every edge whose source node has the given
// RepoPrefix. Implemented as one Cypher MATCH over the (Node)-[Edge]->
// pattern with a source-side repo_prefix filter — equivalent to the
// GetRepoNodes × GetOutEdges nested walk callers used before, but
// drives the join inside the engine. Eliminates the per-source-node
// query round-trip that dominates Ladybug warmup on multi-repo
// workspaces (one extractor call against gortex's ~68k repo nodes
// previously fired ~68k Cypher queries).
func (s *Store) GetRepoEdges(repoPrefix string) []*graph.Edge {
	if repoPrefix == "" {
		return nil
	}
	const q = `MATCH (a:Node {repo_prefix: $r})-[e:Edge]->(b:Node) RETURN ` + edgeReturnCols
	rows := s.querySelect(q, map[string]any{"r": repoPrefix})
	return rowsToEdges(rows)
}

// GetInEdges returns every edge whose To matches nodeID.
//
// The target predicate is expressed as `WHERE b.id = $id`, not an
// inline `(b:Node {id: $id})` property match on the arrow target.
// On a populated workspace the inline form silently returns zero rows
// — the Kuzu planner skips the primary-key probe on the rel-table
// target side and the join collapses to empty. Find_usages /
// get_callers / analyze[cycles] / suggest_pattern all funnel through
// this single primitive, so the empty result cascades into a
// false-positive "no incoming references" verdict across the agent
// surface. Aligning the shape with GetInEdgesByNodeIDs' working
// `WHERE b.id IN $ids` keeps the planner on the same code path that
// the batched sibling exercises (and that the conformance suite
// covers).
func (s *Store) GetInEdges(nodeID string) []*graph.Edge {
	const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE b.id = $id RETURN ` + edgeReturnCols
	rows := s.querySelect(q, map[string]any{"id": nodeID})
	return rowsToEdges(rows)
}

// GetOutEdgesByNodeIDs returns a map id→outgoing edges for every input
// id. One Cypher round-trip drives a `WHERE a.id IN $ids` match — the
// rerank hot path collapses ~30 per-candidate GetOutEdges calls into
// this single batched query (15ms cgo round-trip × 30 = ~450ms saved
// per search_symbols on ladybug). Missing nodes are absent from the
// returned map; empty input returns nil.
func (s *Store) GetOutEdgesByNodeIDs(ids []string) map[string][]*graph.Edge {
	if len(ids) == 0 {
		return nil
	}
	uniq := dedupeNonEmpty(ids)
	if len(uniq) == 0 {
		return nil
	}
	const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE a.id IN $ids RETURN ` + edgeReturnCols
	rows := s.querySelect(q, map[string]any{"ids": stringSliceToAny(uniq)})
	out := make(map[string][]*graph.Edge, len(uniq))
	for _, r := range rows {
		e := rowToEdge(r)
		if e == nil {
			continue
		}
		out[e.From] = append(out[e.From], e)
	}
	return out
}

// GetInEdgesByNodeIDs is the inbound sibling of GetOutEdgesByNodeIDs.
// See that doc-comment for the contract.
func (s *Store) GetInEdgesByNodeIDs(ids []string) map[string][]*graph.Edge {
	if len(ids) == 0 {
		return nil
	}
	uniq := dedupeNonEmpty(ids)
	if len(uniq) == 0 {
		return nil
	}
	const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE b.id IN $ids RETURN ` + edgeReturnCols
	rows := s.querySelect(q, map[string]any{"ids": stringSliceToAny(uniq)})
	out := make(map[string][]*graph.Edge, len(uniq))
	for _, r := range rows {
		e := rowToEdge(r)
		if e == nil {
			continue
		}
		out[e.To] = append(out[e.To], e)
	}
	return out
}

// AllNodes materialises every node into a slice.
func (s *Store) AllNodes() []*graph.Node {
	const q = `MATCH (n:Node) RETURN ` + nodeReturnCols
	rows := s.querySelect(q, nil)
	return rowsToNodes(rows)
}

// AllEdges materialises every edge into a slice.
func (s *Store) AllEdges() []*graph.Edge {
	const q = `MATCH (a:Node)-[e:Edge]->(b:Node) RETURN ` + edgeReturnCols
	rows := s.querySelect(q, nil)
	return rowsToEdges(rows)
}

// EdgesByKind yields every edge whose Kind matches. The query
// materialises into a slice before yielding so the caller's body is
// free to make re-entrant store calls (the connection is held
// exclusively by an open kuzu_query_result and a re-entrant write
// would deadlock).
func (s *Store) EdgesByKind(kind graph.EdgeKind) iter.Seq[*graph.Edge] {
	return func(yield func(*graph.Edge) bool) {
		const q = `MATCH (a:Node)-[e:Edge {kind: $kind}]->(b:Node) RETURN ` + edgeReturnCols
		rows := s.querySelect(q, map[string]any{"kind": string(kind)})
		for _, r := range rows {
			e := rowToEdge(r)
			if e == nil {
				continue
			}
			if !yield(e) {
				return
			}
		}
	}
}

// EdgesByKinds yields every edge whose Kind is in the supplied set,
// in a single backend round-trip. One Cypher query with a kind IN-list
// replaces the N independent EdgesByKind queries the edge-driven
// analyzers (channel_ops, pubsub, k8s_resources, kustomize, …)
// otherwise need when they care about 2-5 kinds at once. Materialises
// the row set before yielding for the same reentrancy reason as
// EdgesByKind.
//
// Empty kinds yields nothing — matches the in-memory reference and
// avoids handing Kuzu's planner an empty IN-list (which it tolerates
// but plans badly).
func (s *Store) EdgesByKinds(kinds []graph.EdgeKind) iter.Seq[*graph.Edge] {
	return func(yield func(*graph.Edge) bool) {
		uniq := dedupeEdgeKinds(kinds)
		if len(uniq) == 0 {
			return
		}
		const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE e.kind IN $kinds RETURN ` + edgeReturnCols
		rows := s.querySelect(q, map[string]any{"kinds": edgeKindSliceToAny(uniq)})
		for _, r := range rows {
			e := rowToEdge(r)
			if e == nil {
				continue
			}
			if !yield(e) {
				return
			}
		}
	}
}

// NodesByKind yields every node whose Kind matches.
func (s *Store) NodesByKind(kind graph.NodeKind) iter.Seq[*graph.Node] {
	return func(yield func(*graph.Node) bool) {
		const q = `MATCH (n:Node) WHERE n.kind = $kind RETURN ` + nodeReturnCols
		rows := s.querySelect(q, map[string]any{"kind": string(kind)})
		for _, r := range rows {
			n := rowToNode(r)
			if n == nil {
				continue
			}
			if !yield(n) {
				return
			}
		}
	}
}

// EdgesWithUnresolvedTarget yields every edge whose To begins with
// "unresolved::". The COPY-time rewrite in copyBulkLocked preserves
// this prefix in the multi-repo form (`unresolved::<repoPrefix>::<name>`),
// so a single STARTS WITH still catches every form without paying
// for an index-killing CONTAINS scan.
func (s *Store) EdgesWithUnresolvedTarget() iter.Seq[*graph.Edge] {
	return func(yield func(*graph.Edge) bool) {
		const q = `MATCH (a:Node)-[e:Edge]->(b:Node) WHERE b.id STARTS WITH 'unresolved::' RETURN ` + edgeReturnCols
		rows := s.querySelect(q, nil)
		for _, r := range rows {
			e := rowToEdge(r)
			if e == nil {
				continue
			}
			if !yield(e) {
				return
			}
		}
	}
}

// GetNodesByIDs returns a map id→*Node for every input ID present.
// IDs not in the store are absent from the returned map.
func (s *Store) GetNodesByIDs(ids []string) map[string]*graph.Node {
	if len(ids) == 0 {
		return nil
	}
	uniq := dedupeNonEmpty(ids)
	if len(uniq) == 0 {
		return nil
	}
	// IN $ids on the indexed PK collapses N point lookups into one
	// Cypher statement.
	const q = `MATCH (n:Node) WHERE n.id IN $ids RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"ids": stringSliceToAny(uniq)})
	out := make(map[string]*graph.Node, len(uniq))
	for _, r := range rows {
		n := rowToNode(r)
		if n == nil {
			continue
		}
		out[n.ID] = n
	}
	return out
}

// FindNodesByNames returns a map name→[]*Node for every input name.
// Names that match no node are absent from the returned map.
func (s *Store) FindNodesByNames(names []string) map[string][]*graph.Node {
	if len(names) == 0 {
		return nil
	}
	uniq := dedupeNonEmpty(names)
	if len(uniq) == 0 {
		return nil
	}
	const q = `MATCH (n:Node) WHERE n.name IN $names RETURN ` + nodeReturnCols
	rows := s.querySelect(q, map[string]any{"names": stringSliceToAny(uniq)})
	out := make(map[string][]*graph.Node, len(uniq))
	for _, r := range rows {
		n := rowToNode(r)
		if n == nil {
			continue
		}
		out[n.Name] = append(out[n.Name], n)
	}
	return out
}
