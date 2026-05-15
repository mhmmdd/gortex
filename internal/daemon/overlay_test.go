package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOverlayManager_RegisterAndFiles is the happy-path round trip:
// register a session, push two overlays, list them via SnapshotFor.
// The snapshot must be path-sorted (the apply pass relies on stable
// ordering for reproducible drift errors) and must not alias the
// manager's internal map.
func TestOverlayManager_RegisterAndFiles(t *testing.T) {
	m := NewOverlayManager(time.Minute)
	id := m.Register("ws")
	require.NotEmpty(t, id)

	require.NoError(t, m.Push(id, OverlayFile{Path: "b.go", Content: "package b"}, nil))
	require.NoError(t, m.Push(id, OverlayFile{Path: "a.go", Content: "package a"}, nil))

	ws, files, err := m.SnapshotFor(id)
	require.NoError(t, err)
	require.Equal(t, "ws", ws)
	require.Len(t, files, 2)
	require.Equal(t, "a.go", files[0].Path, "snapshot must be path-sorted")
	require.Equal(t, "b.go", files[1].Path)

	// Snapshot must not alias the internal map.
	files[0].Content = "mutated"
	_, again, _ := m.SnapshotFor(id)
	require.Equal(t, "package a", again[0].Content, "SnapshotFor must return a deep copy")
}

// TestOverlayManager_RegisterWithID_Idempotent verifies the MCP-side
// register flow: calling RegisterWithID twice for the same (sessionID,
// workspaceID) tuple is a no-op, but a workspace mismatch is rejected
// with ErrSessionExists. Without this contract the MCP overlay_register
// tool would have to teach every editor extension to track register
// state across reconnects.
func TestOverlayManager_RegisterWithID_Idempotent(t *testing.T) {
	m := NewOverlayManager(time.Minute)
	require.NoError(t, m.RegisterWithID("sess-1", "ws-a"))
	require.NoError(t, m.RegisterWithID("sess-1", "ws-a"), "idempotent re-register must succeed")

	err := m.RegisterWithID("sess-1", "ws-b")
	require.ErrorIs(t, err, ErrSessionExists, "workspace mismatch must surface ErrSessionExists")

	// Empty session ID is a programming error.
	require.Error(t, m.RegisterWithID("", "ws-a"))
}

// TestOverlayManager_HasAndFileCount covers the dispatcher's fast-path
// gating: tools/call middleware bails before any apply work when
// Has==false or FileCount==0.
func TestOverlayManager_HasAndFileCount(t *testing.T) {
	m := NewOverlayManager(time.Minute)
	require.False(t, m.Has("unknown"))
	require.Zero(t, m.FileCount("unknown"))

	id := m.Register("ws")
	require.True(t, m.Has(id))
	require.Zero(t, m.FileCount(id), "freshly registered session has no files")

	require.NoError(t, m.Push(id, OverlayFile{Path: "x.go", Content: "x"}, nil))
	require.Equal(t, 1, m.FileCount(id))

	m.Drop(id)
	require.False(t, m.Has(id))
	require.Zero(t, m.FileCount(id))
}

// TestOverlayManager_DriftCheck verifies that Push surfaces a drift
// error when the supplied BaseSHA disagrees with the on-disk SHA
// reported by the callback. Without drift detection two clients
// pushing stale overlays against the same path would silently corrupt
// each other's query results.
func TestOverlayManager_DriftCheck(t *testing.T) {
	m := NewOverlayManager(time.Minute)
	id := m.Register("ws")

	// BaseSHA matches: push succeeds.
	require.NoError(t, m.Push(id,
		OverlayFile{Path: "x.go", Content: "x", BaseSHA: "abc"},
		func(path, sha string) bool { return sha == "abc" },
	))

	// BaseSHA mismatches: ErrOverlayDrift.
	err := m.Push(id,
		OverlayFile{Path: "x.go", Content: "x", BaseSHA: "stale"},
		func(path, sha string) bool { return sha == "abc" },
	)
	require.True(t, errors.Is(err, ErrOverlayDrift))
}

// TestOverlayManager_SweepIdleHonoursTTL ensures that sessions older
// than IdleTTL are reaped. A crashed editor extension leaving overlays
// in the daemon would otherwise pin memory until restart.
func TestOverlayManager_SweepIdleHonoursTTL(t *testing.T) {
	m := NewOverlayManager(20 * time.Millisecond)
	id := m.Register("ws")
	require.True(t, m.Has(id))

	time.Sleep(40 * time.Millisecond)
	dropped := m.SweepIdle()
	require.Equal(t, 1, dropped, "session past idleTTL must be reaped")
	require.False(t, m.Has(id))
}
