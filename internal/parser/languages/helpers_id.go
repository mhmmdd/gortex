package languages

import "strconv"

// disambiguateID resolves an in-file node-ID collision: it returns baseID when
// that ID is still free within the file and a line-suffixed variant
// (baseID_L<startLine>) when baseID is already taken by a declaration on a
// different line, so two declarations that would otherwise share an ID — two
// func init(), an @overload set, a redefined method, a conditionally-compiled
// twin — both survive as distinct, navigable nodes instead of one silently
// overwriting the other (which also orphans the loser's edges).
//
// ok is false for an exact re-match — the SAME declaration (same base ID at the
// same start line) seen twice, e.g. one definition matched by two overlapping
// tree-sitter query patterns — which the caller should drop rather than
// disambiguate, preserving the existing dedup behaviour. Genuine collisions
// (same name, different line) are kept.
//
// seen is the per-file set the helper reads and updates; pass the SAME map
// through every ID-construction site for one file. It is safe to share with an
// extractor's existing dedup set: the per-declaration markers the helper writes
// carry a '#L' separator that never appears in a real node ID.
//
// This generalizes the per-language cpp/csharp/java/dart precedent into one
// shared helper while keeping Gortex's human-readable IDs intact — a
// non-colliding symbol pays nothing and only a genuine duplicate gains the
// suffix, rather than hashing every ID into an opaque token to buy
// collision-freedom.
func disambiguateID(seen map[string]bool, baseID string, startLine int) (id string, ok bool) {
	decl := baseID + "#L" + strconv.Itoa(startLine)
	if seen[decl] {
		return baseID, false
	}
	seen[decl] = true
	id = baseID
	if seen[id] {
		id = baseID + "_L" + strconv.Itoa(startLine)
	}
	seen[id] = true
	return id, true
}
