package mcp

import (
	"encoding/json"
	"testing"
)

// TestRecoverableNotTrackedReturnsSuccessGuidance proves the F3 guardrail: a
// recoverable condition (repo not tracked, symbol not found, file not indexed)
// returns a NON-error result carrying machine-readable guidance — never a
// session-abandoning isError — and the guidance routes to Gortex's richer
// escape hatches plus the structured `gortex track` affordance.
func TestRecoverableNotTrackedReturnsSuccessGuidance(t *testing.T) {
	decode := func(t *testing.T, body string) RecoverableGuidance {
		t.Helper()
		var g RecoverableGuidance
		if err := json.Unmarshal([]byte(body), &g); err != nil {
			t.Fatalf("guidance body is not JSON: %v\n%s", err, body)
		}
		return g
	}

	t.Run("repo_not_tracked", func(t *testing.T) {
		res := repoNotTrackedGuidance("/work/newrepo")
		if res.IsError {
			t.Fatal("repo-not-tracked must NOT be an isError result (recoverable)")
		}
		g := decode(t, toolResultText(res))
		if !g.Recoverable || g.Condition != ErrCodeRepoNotTracked {
			t.Errorf("guidance = %+v, want recoverable repo_not_tracked", g)
		}
		if g.TrackCommand != "gortex track /work/newrepo" {
			t.Errorf("track_command = %q, want the path-specific gortex track", g.TrackCommand)
		}
		if !containsString(g.SuggestedTools, "find_files") || !containsString(g.SuggestedTools, "search_text") {
			t.Errorf("suggested_tools = %v, want the content-search escape hatches", g.SuggestedTools)
		}
	})

	t.Run("symbol_not_found", func(t *testing.T) {
		res := symbolNotFoundGuidance("pkg/foo.go::Bar")
		if res.IsError {
			t.Fatal("symbol-not-found must NOT be an isError result")
		}
		g := decode(t, toolResultText(res))
		if g.Condition != ErrCodeSymbolNotFound {
			t.Errorf("condition = %q, want symbol_not_found", g.Condition)
		}
		// Routes to Gortex's richer locators, not just three tools.
		if !containsString(g.SuggestedTools, "search_symbols") || !containsString(g.SuggestedTools, "find_usages") {
			t.Errorf("suggested_tools = %v, want search_symbols + find_usages", g.SuggestedTools)
		}
		if g.Data["id"] != "pkg/foo.go::Bar" {
			t.Errorf("data.id = %v, want the queried id", g.Data["id"])
		}
	})

	t.Run("file_not_indexed", func(t *testing.T) {
		res := fileNotIndexedGuidance("internal/new.go")
		if res.IsError {
			t.Fatal("file-not-indexed must NOT be an isError result")
		}
		g := decode(t, toolResultText(res))
		if g.Condition != ErrCodeFileNotIndexed {
			t.Errorf("condition = %q, want file_not_indexed", g.Condition)
		}
		if !containsString(g.SuggestedTools, "read_file") {
			t.Errorf("suggested_tools = %v, want read_file as a fallback", g.SuggestedTools)
		}
	})
}

func containsString(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
