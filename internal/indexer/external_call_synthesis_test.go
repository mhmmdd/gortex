package indexer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zzet/gortex/internal/graph"
)

func TestExternalCallSynthesisEnabled_EnvOverride(t *testing.T) {
	g := graph.New()
	idx := newTestIndexer(g)
	// Default-off: neither config nor env set.
	require.False(t, idx.externalCallSynthesisEnabled())

	idx.config.SynthesizeExternalCalls = true
	require.True(t, idx.externalCallSynthesisEnabled())

	t.Setenv("GORTEX_SYNTH_EXTERNAL_CALLS", "0")
	require.False(t, idx.externalCallSynthesisEnabled()) // env overrides config-on

	t.Setenv("GORTEX_SYNTH_EXTERNAL_CALLS", "1")
	idx.config.SynthesizeExternalCalls = false
	require.True(t, idx.externalCallSynthesisEnabled()) // env overrides config-off
}
