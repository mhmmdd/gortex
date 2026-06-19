package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// Recoverable-condition UX.
//
// Some tool conditions are not failures of the SERVER — they are states the
// agent can recover from on its own: the cwd isn't a tracked repo, a symbol id
// isn't in the index, a file has no indexed symbols. Returning those as an
// isError CallToolResult makes well-behaved clients treat the turn as failed
// and, in the worst case, abandon the session. F3's rule: such conditions
// return a NORMAL (non-isError) result carrying machine-readable guidance — the
// condition code, a human message, the next tools to try, and a `gortex track`
// affordance — so the agent reroutes instead of giving up. isError is reserved
// for security refusals and genuine malfunctions.

// RecoverableGuidance is the success-shaped counterpart to StructuredError. The
// `recoverable: true` flag and `condition` code let a smart client branch; the
// suggested_tools and track_command give a plain agent its next move.
type RecoverableGuidance struct {
	Recoverable    bool           `json:"recoverable"`
	Condition      ErrorCode      `json:"condition"`
	Message        string         `json:"message"`
	SuggestedTools []string       `json:"suggested_tools,omitempty"`
	TrackCommand   string         `json:"track_command,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

// newRecoverableResult encodes guidance into a NON-error result. The JSON body
// is the machine-readable form; IsError stays false so no client treats a
// recoverable state as a session-ending failure.
func newRecoverableResult(g RecoverableGuidance) *mcp.CallToolResult {
	g.Recoverable = true
	body, err := json.Marshal(g)
	if err != nil {
		return mcp.NewToolResultText(g.Message)
	}
	res := mcp.NewToolResultText(string(body))
	res.IsError = false
	return res
}

// repoNotTrackedGuidance: the path isn't covered by any tracked repo. Routes to
// the content-search escape hatches and offers the exact `gortex track` command.
func repoNotTrackedGuidance(path string) *mcp.CallToolResult {
	track := "gortex track ."
	if path != "" {
		track = "gortex track " + path
	}
	return newRecoverableResult(RecoverableGuidance{
		Condition:      ErrCodeRepoNotTracked,
		Message:        fmt.Sprintf("%q is not covered by any tracked repository, so the graph has nothing to answer with yet — track it, or fall back to a content search.", path),
		SuggestedTools: []string{"find_files", "search_text"},
		TrackCommand:   track,
		Data:           map[string]any{"path": path},
	})
}

// symbolNotFoundGuidance: the symbol id isn't in the index. Routes to the
// name/usage/text searches that can locate it (or confirm it's a local).
func symbolNotFoundGuidance(id string) *mcp.CallToolResult {
	return newRecoverableResult(RecoverableGuidance{
		Condition:      ErrCodeSymbolNotFound,
		Message:        fmt.Sprintf("no symbol with id %q is in the index — it may be spelled differently, live in an unindexed file, or be a local. Search for it rather than reading by id.", id),
		SuggestedTools: []string{"search_symbols", "find_usages", "search_text"},
		Data:           map[string]any{"id": id},
	})
}

// fileNotIndexedGuidance: a file has no indexed symbols. Routes to file/content
// discovery and a raw read.
func fileNotIndexedGuidance(path string) *mcp.CallToolResult {
	return newRecoverableResult(RecoverableGuidance{
		Condition:      ErrCodeFileNotIndexed,
		Message:        fmt.Sprintf("no symbols are indexed for %q — the file may be new, ignored, or in a language without an extractor. Find or read it directly instead.", path),
		SuggestedTools: []string{"find_files", "search_text", "read_file"},
		Data:           map[string]any{"path": path},
	})
}
