package resolver

import (
	"os"
	"strings"
)

// backendResolverEnabled reports whether the resolver should consult
// graph.BackendResolver before running its Go-side worker pool. Off
// by default — the in-memory shadow path (gortex / vscode / repos
// under 50k files) already resolves in RAM at nanosecond latency,
// so backend delegation would only add round-trips. Opt in via
// GORTEX_BACKEND_RESOLVER=1 (or "true") for the large-repo, disk-
// only path where the shadow swap is disabled and per-edge round-
// trips dominate the resolve phase.
func backendResolverEnabled() bool {
	v := os.Getenv("GORTEX_BACKEND_RESOLVER")
	return v == "1" || strings.EqualFold(v, "true")
}
