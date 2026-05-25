//go:build ladybug

package store_ladybug

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zzet/gortex/internal/graph"
)

func TestVectorSearcher_BulkAndQuery(t *testing.T) {
	dir, err := os.MkdirTemp("", "lbug-vec-bulk-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s, err := Open(filepath.Join(dir, "store.lbug"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	items := []graph.VectorItem{
		{NodeID: "alpha", Vec: []float32{1, 0, 0, 0}},
		{NodeID: "alpha_neighbor", Vec: []float32{0.95, 0.05, 0, 0}},
		{NodeID: "orthogonal", Vec: []float32{0, 1, 0, 0}},
		{NodeID: "opposite", Vec: []float32{-1, 0, 0, 0}},
	}
	require.NoError(t, s.BulkUpsertEmbeddings(items))
	require.NoError(t, s.BuildVectorIndex(4))

	hits, err := s.SimilarTo([]float32{1, 0, 0, 0}, 3)
	require.NoError(t, err)
	require.Len(t, hits, 3, "k=3 must return 3 hits")
	// alpha (identical) should rank first; alpha_neighbor second;
	// orthogonal third (cosine distance 1.0 > opposite's 2.0? — let
	// the engine decide ordering, but assert that alpha and
	// alpha_neighbor are the first two regardless of orientation).
	topIDs := map[string]bool{hits[0].NodeID: true, hits[1].NodeID: true}
	assert.True(t, topIDs["alpha"], "exact match must be in the top two; got hits=%v", hits)
	assert.True(t, topIDs["alpha_neighbor"], "near neighbour must be in the top two; got hits=%v", hits)
	assert.InDelta(t, 0.0, hits[0].Distance, 0.001, "top hit distance must be near zero for the exact-match query")
}

func TestVectorSearcher_PerCallUpsert(t *testing.T) {
	dir, err := os.MkdirTemp("", "lbug-vec-per-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s, err := Open(filepath.Join(dir, "store.lbug"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.UpsertEmbedding("a", []float32{1, 0, 0, 0}))
	require.NoError(t, s.UpsertEmbedding("b", []float32{0, 1, 0, 0}))

	hits, err := s.SimilarTo([]float32{1, 0, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.Equal(t, "a", hits[0].NodeID)
}

// TestVectorSearcher_DimRejectsMismatch guards the index dim
// contract — every Upsert / Bulk must match the declared
// FLOAT[N] column width.
func TestVectorSearcher_DimRejectsMismatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "lbug-vec-dim-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s, err := Open(filepath.Join(dir, "store.lbug"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.UpsertEmbedding("a", []float32{1, 0, 0, 0}))

	// Second upsert with the wrong dim must error rather than
	// silently truncate / pad.
	err = s.UpsertEmbedding("b", []float32{1, 0, 0})
	require.Error(t, err)
}

// TestVectorSearcher_BulkReplacesPriorCorpus confirms the bulk
// path's wipe-and-rewrite semantics — re-running with a smaller
// set drops the prior rows.
func TestVectorSearcher_BulkReplacesPriorCorpus(t *testing.T) {
	dir, err := os.MkdirTemp("", "lbug-vec-replace-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s, err := Open(filepath.Join(dir, "store.lbug"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.BulkUpsertEmbeddings([]graph.VectorItem{
		{NodeID: "a", Vec: []float32{1, 0, 0, 0}},
		{NodeID: "b", Vec: []float32{0, 1, 0, 0}},
		{NodeID: "c", Vec: []float32{0, 0, 1, 0}},
	}))
	require.NoError(t, s.BuildVectorIndex(4))

	hits, err := s.SimilarTo([]float32{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, hits, 3, "initial bulk should land 3 rows")

	// Second bulk with one row only.
	require.NoError(t, s.BulkUpsertEmbeddings([]graph.VectorItem{
		{NodeID: "z", Vec: []float32{1, 1, 0, 0}},
	}))
	require.NoError(t, s.BuildVectorIndex(4))

	hits, err = s.SimilarTo([]float32{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1, "wipe-and-rewrite must drop prior rows; got %v", hits)
	assert.Equal(t, "z", hits[0].NodeID)
}
