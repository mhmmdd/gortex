//go:build cozo

package store_cozo

import (
	"fmt"

	cozo "github.com/cozodb/cozo-lib-go"

	"github.com/zzet/gortex/internal/graph"
)

// Compile-time assertion: *Store satisfies graph.BackendResolver.
var _ graph.BackendResolver = (*Store)(nil)

// Cozo Datalog implementations of the bulk-resolve passes.
//
// Cozo's std lib has no substring function — so we extract the
// embedded name via the equivalent constraint
// `to_id_old == concat('unresolved::', name)`, which the
// Datalog planner solves by joining against the candidate Node's
// name column. Aggregation goes in the rule head:
//   ?[group_col, count(value_col)] := body
// produces one row per distinct group_col with the count.
//
// All mutations: query → :rm old keys → :put new rows under one
// writeMu hold.

const (
	cozoEdgePutSchema = "from_id, to_id, kind, file_path, line => confidence, confidence_label, origin, tier, cross_repo, meta"
	cozoRmEdgeQuery   = `?[from_id, to_id, kind, file_path, line] <- $rows :rm edge {from_id, to_id, kind, file_path, line}`
	cozoPutEdgeQuery  = `?[from_id, to_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] <- $rows :put edge {` + cozoEdgePutSchema + `}`
)

// rewriteEdgesByQuery runs `findQuery` (returns columns
// old_to_id, from_id, target_id, kind, file_path, line,
// confidence, confidence_label, origin, tier, cross_repo, meta —
// in that order) and rewrites each row's edge.
func (s *Store) rewriteEdgesByQuery(findQuery, ruleName string) (int, error) {
	res, err := s.db.Run(findQuery, cozo.Map{})
	if err != nil {
		return 0, fmt.Errorf("backend-resolver %s find: %w", ruleName, err)
	}
	if !res.Ok || len(res.Rows) == 0 {
		return 0, nil
	}
	rmRows := make([][]any, 0, len(res.Rows))
	putRows := make([][]any, 0, len(res.Rows))
	for _, r := range res.Rows {
		if len(r) < 12 {
			continue
		}
		oldTo := asString(r[0])
		from := asString(r[1])
		newTo := asString(r[2])
		kind := asString(r[3])
		filePath := asString(r[4])
		line := asInt(r[5])
		confidence := asFloat(r[6])
		confLabel := asString(r[7])
		_ = asString(r[8]) // origin (overwritten)
		_ = asString(r[9]) // tier (overwritten)
		crossRepo := asBool(r[10])
		meta := asString(r[11])
		rmRows = append(rmRows, []any{from, oldTo, kind, filePath, line})
		putRows = append(putRows, []any{
			from, newTo, kind, filePath, line,
			confidence, confLabel, "ast_resolved", "ast_resolved", crossRepo, meta,
		})
	}
	if len(rmRows) == 0 {
		return 0, nil
	}
	if _, err := s.db.Run(cozoRmEdgeQuery, cozo.Map{"rows": rmRows}); err != nil {
		return 0, fmt.Errorf("backend-resolver %s rm: %w", ruleName, err)
	}
	if _, err := s.db.Run(cozoPutEdgeQuery, cozo.Map{"rows": putRows}); err != nil {
		return 0, fmt.Errorf("backend-resolver %s put: %w", ruleName, err)
	}
	s.edgeIdentityRevs.Add(int64(len(rmRows)))
	return len(rmRows), nil
}

// ResolveSameFile: caller and target share file_path.
func (s *Store) ResolveSameFile() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	const q = `
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id_old, 'unresolved::'),
    *node{id: from_id, file_path: caller_file},
    caller_file != '',
    *node{id: target_id, name, file_path: caller_file},
    to_id_old == concat('unresolved::', name),
    target_id != to_id_old

cand_counts[from_id, to_id_old, count(target_id)] :=
    candidates[from_id, to_id_old, target_id, _, _, _, _, _, _, _, _, _]

unique_edges[from_id, to_id_old] :=
    cand_counts[from_id, to_id_old, cnt],
    cnt == 1

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta],
    unique_edges[from_id, to_id_old]
`
	return s.rewriteEdgesByQuery(q, "ResolveSameFile")
}

// ResolveSamePackage: same directory + same repo_prefix.
// Uses regex_replace to derive the directory (everything before
// the last "/").
func (s *Store) ResolveSamePackage() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	const q = `
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id_old, 'unresolved::'),
    *node{id: from_id, file_path: caller_file, repo_prefix: caller_repo},
    caller_file != '',
    str_includes(caller_file, '/'),
    caller_dir = regex_replace(caller_file, '/[^/]+$', ''),
    *node{id: target_id, name, file_path: target_file, repo_prefix: target_repo},
    to_id_old == concat('unresolved::', name),
    target_id != to_id_old,
    target_file != caller_file,
    target_repo == caller_repo,
    str_includes(target_file, '/'),
    regex_replace(target_file, '/[^/]+$', '') == caller_dir

cand_counts[from_id, to_id_old, count(target_id)] :=
    candidates[from_id, to_id_old, target_id, _, _, _, _, _, _, _, _, _]

unique_edges[from_id, to_id_old] :=
    cand_counts[from_id, to_id_old, cnt],
    cnt == 1

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta],
    unique_edges[from_id, to_id_old]
`
	return s.rewriteEdgesByQuery(q, "ResolveSamePackage")
}

// ResolveImportAware: caller's file imports F, target lives in F.
func (s *Store) ResolveImportAware() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	const q = `
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id_old, 'unresolved::'),
    *node{id: from_id, file_path: caller_file},
    caller_file != '',
    *node{id: caller_file_node, file_path: caller_file, kind: 'file'},
    *edge{from_id: caller_file_node, to_id: imported_file_node, kind: 'imports'},
    *node{id: imported_file_node, kind: 'file', file_path: imported_file_path},
    not starts_with(imported_file_node, 'external::'),
    not starts_with(imported_file_node, 'unresolved::'),
    *node{id: target_id, name, file_path: imported_file_path},
    to_id_old == concat('unresolved::', name),
    target_id != to_id_old

cand_counts[from_id, to_id_old, count(target_id)] :=
    candidates[from_id, to_id_old, target_id, _, _, _, _, _, _, _, _, _]

unique_edges[from_id, to_id_old] :=
    cand_counts[from_id, to_id_old, cnt],
    cnt == 1

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta],
    unique_edges[from_id, to_id_old]
`
	return s.rewriteEdgesByQuery(q, "ResolveImportAware")
}

// ResolveRelativeImports: pyrel::<stem> → <stem>.py or
// <stem>/__init__.py.
func (s *Store) ResolveRelativeImports(lang string) (int, error) {
	if lang != "" && lang != "python" {
		return 0, nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var total int
	for _, suffix := range []string{".py", "/__init__.py"} {
		// Cozo's Datalog doesn't invert concat to solve for the
		// stem variable, so we derive it via regex_replace on the
		// target_id (strip the suffix). Then concat with the
		// pyrel prefix to match against to_id_old.
		suffixEsc := suffix
		if suffixEsc == ".py" {
			suffixEsc = "\\.py$"
		} else {
			suffixEsc = "/__init__\\.py$"
		}
		q := fmt.Sprintf(`
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    kind == 'imports',
    starts_with(to_id_old, 'unresolved::pyrel::'),
    *node{id: target_id, kind: 'file'},
    ends_with(target_id, %q),
    stem = regex_replace(target_id, %q, ''),
    to_id_old == concat('unresolved::pyrel::', stem)

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta]
`, suffix, suffixEsc)
		n, err := s.rewriteEdgesByQuery(q, "ResolveRelativeImports "+suffix)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// ResolveCrossRepo: unique cross-repo same-name candidate.
func (s *Store) ResolveCrossRepo() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	const q = `
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id_old, 'unresolved::'),
    *node{id: from_id, repo_prefix: caller_repo},
    caller_repo != '',
    *node{id: target_id, name, repo_prefix: target_repo},
    to_id_old == concat('unresolved::', name),
    target_repo != caller_repo,
    target_repo != '',
    target_id != to_id_old

cand_counts[from_id, to_id_old, count(target_id)] :=
    candidates[from_id, to_id_old, target_id, _, _, _, _, _, _, _, _, _]

unique_edges[from_id, to_id_old] :=
    cand_counts[from_id, to_id_old, cnt],
    cnt == 1

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo_orig, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, _, meta],
    unique_edges[from_id, to_id_old],
    cross_repo_orig = true
`
	return s.rewriteEdgesByQuery(q, "ResolveCrossRepo")
}

// ResolveUniqueNames: unambiguous-by-uniqueness fallback.
func (s *Store) ResolveUniqueNames() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	const q = `
candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    *edge{from_id, to_id: to_id_old, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id_old, 'unresolved::'),
    *node{id: target_id, name},
    to_id_old == concat('unresolved::', name),
    target_id != to_id_old

cand_counts[from_id, to_id_old, count(target_id)] :=
    candidates[from_id, to_id_old, target_id, _, _, _, _, _, _, _, _, _]

unique_edges[from_id, to_id_old] :=
    cand_counts[from_id, to_id_old, cnt],
    cnt == 1

?[to_id_old, from_id, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta] :=
    candidates[from_id, to_id_old, target_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta],
    unique_edges[from_id, to_id_old]
`
	return s.rewriteEdgesByQuery(q, "ResolveUniqueNames")
}

// ResolveExternalCallStubs: create Node rows for external::* targets
// and promote edge origin.
func (s *Store) ResolveExternalCallStubs() (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Step 1: find external::* edge targets missing a Node row.
	// Build name by stripping the prefix via concat-join.
	const findStubs = `
needed[stub_id, name] :=
    *edge{to_id: stub_id},
    starts_with(stub_id, 'external::'),
    name = regex_replace(stub_id, '^external::', ''),
    not *node{id: stub_id}

?[id, kind, name, qual_name, file_path, start_line, end_line, language,
  repo_prefix, workspace_id, project_id, absolute_file_path, meta] :=
    needed[id, name],
    kind = 'external',
    qual_name = '',
    file_path = '',
    start_line = 0,
    end_line = 0,
    language = '',
    repo_prefix = '',
    workspace_id = '',
    project_id = '',
    absolute_file_path = '',
    meta = ''
`
	stubsRes, err := s.db.Run(findStubs, cozo.Map{})
	if err != nil {
		return 0, fmt.Errorf("backend-resolver ResolveExternalCallStubs find: %w", err)
	}
	if stubsRes.Ok && len(stubsRes.Rows) > 0 {
		const putStubs = `
?[id, kind, name, qual_name, file_path, start_line, end_line, language,
  repo_prefix, workspace_id, project_id, absolute_file_path, meta] <- $rows
:put node {
    id =>
    kind, name, qual_name, file_path, start_line, end_line, language,
    repo_prefix, workspace_id, project_id, absolute_file_path, meta
}`
		rows := make([][]any, 0, len(stubsRes.Rows))
		for _, r := range stubsRes.Rows {
			rows = append(rows, r)
		}
		if _, err := s.db.Run(putStubs, cozo.Map{"rows": rows}); err != nil {
			return 0, fmt.Errorf("backend-resolver ResolveExternalCallStubs put: %w", err)
		}
	}

	// Step 2: promote origin/tier on every external::* edge with
	// empty origin. :rm + :put under one lock.
	const findPromote = `
?[from_id, to_id, kind, file_path, line, confidence, confidence_label, cross_repo, meta] :=
    *edge{from_id, to_id, kind, file_path, line, confidence, confidence_label, origin, tier, cross_repo, meta},
    starts_with(to_id, 'external::'),
    origin == ''
`
	promoteRes, err := s.db.Run(findPromote, cozo.Map{})
	if err != nil {
		return 0, fmt.Errorf("backend-resolver ResolveExternalCallStubs find-promote: %w", err)
	}
	if !promoteRes.Ok || len(promoteRes.Rows) == 0 {
		return 0, nil
	}
	rmRows := make([][]any, 0, len(promoteRes.Rows))
	putRows := make([][]any, 0, len(promoteRes.Rows))
	for _, r := range promoteRes.Rows {
		if len(r) < 9 {
			continue
		}
		from := asString(r[0])
		to := asString(r[1])
		kind := asString(r[2])
		filePath := asString(r[3])
		line := asInt(r[4])
		confidence := asFloat(r[5])
		confLabel := asString(r[6])
		crossRepo := asBool(r[7])
		meta := asString(r[8])
		rmRows = append(rmRows, []any{from, to, kind, filePath, line})
		putRows = append(putRows, []any{
			from, to, kind, filePath, line,
			confidence, confLabel, "ast_resolved", "ast_resolved", crossRepo, meta,
		})
	}
	if _, err := s.db.Run(cozoRmEdgeQuery, cozo.Map{"rows": rmRows}); err != nil {
		return 0, fmt.Errorf("backend-resolver ResolveExternalCallStubs rm: %w", err)
	}
	if _, err := s.db.Run(cozoPutEdgeQuery, cozo.Map{"rows": putRows}); err != nil {
		return 0, fmt.Errorf("backend-resolver ResolveExternalCallStubs put: %w", err)
	}
	s.edgeIdentityRevs.Add(int64(len(rmRows)))
	return len(rmRows), nil
}

// ResolveAllBulk runs every rule in precision-descending order.
func (s *Store) ResolveAllBulk() (int, error) {
	var total int
	for _, fn := range []func() (int, error){
		s.ResolveSameFile,
		s.ResolveSamePackage,
		s.ResolveImportAware,
		func() (int, error) { return s.ResolveRelativeImports("") },
		s.ResolveCrossRepo,
		s.ResolveUniqueNames,
		s.ResolveExternalCallStubs,
	} {
		n, err := fn()
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
