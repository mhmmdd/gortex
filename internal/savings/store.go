// Package savings persists cumulative token-savings metrics across server
// restarts. Every source-reading tool call feeds this store through the MCP
// server's tokenStats, so over time the numbers become a credible narrative:
// "Gortex saved N tokens / $X at model rate this month".
//
// Storage: the machine-global SQLite sidecar (~/.gortex/sidecar.sqlite —
// the same database that holds notes, memories, scopes, and notebooks).
// Every observation is one transaction (event row + totals upsert), so the
// ledger is durable at the call: a SIGKILLed server loses nothing, and
// multiple gortex processes write the same database safely through SQLite's
// WAL + busy-timeout. The flat-file era (savings.json cumulative totals +
// savings.jsonl event log under the cache dir) is imported once on open and
// the legacy files renamed to *.bak.
package savings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zzet/gortex/internal/persistence"
	"github.com/zzet/gortex/internal/platform"
)

// schemaVersion is the snapshot-shape version, kept for the JSON surface
// (graph_stats cumulative_savings, `gortex savings --json`) and for
// reading flat-file-era ledgers during legacy import.
const schemaVersion = 1

// Totals is the cumulative record for a single scope (top-level or per-repo).
type Totals struct {
	TokensSaved    int64 `json:"tokens_saved"`
	TokensReturned int64 `json:"tokens_returned"`
	CallsCounted   int64 `json:"calls_counted"`
}

// File is the snapshot shape callers consume (graph_stats, the CLI) and
// the on-disk schema of the flat-file era — still parsed by the one-shot
// legacy import.
type File struct {
	Version     int                `json:"version"`
	FirstSeen   time.Time          `json:"first_seen"`
	LastUpdated time.Time          `json:"last_updated"`
	Totals      Totals             `json:"totals"`
	PerRepo     map[string]*Totals `json:"per_repo,omitempty"`
	PerLanguage map[string]*Totals `json:"per_language,omitempty"`
}

// Observation is one source-reading tool call to book.
type Observation struct {
	Repo      string
	Language  string
	Tool      string
	SessionID string
	Returned  int64
	Saved     int64
}

// Store is the token-savings ledger. All operations are safe for
// concurrent use. When opened with an empty path the store tracks
// in-memory only — the behaviour test fixtures and the eval servers
// rely on — and never touches disk.
//
// Write errors against the sidecar are intentionally not propagated to
// record() callers (accounting must never fail a tool call), but unlike
// the flat-file era there is no batching: an observation either commits
// durably or is dropped whole.
type Store struct {
	mu        sync.Mutex
	sc        *persistence.SidecarStore
	mem       File    // in-memory accumulation when sc == nil
	memEvents []Event // in-memory event log when sc == nil
}

// DefaultDBPath returns the canonical savings ledger location: the
// machine-global sidecar database under the Gortex data dir.
func DefaultDBPath() string {
	return persistence.DefaultSidecarPath(platform.DataDir())
}

// DefaultPath returns the flat-file era's savings.json location under the
// Gortex cache dir. The live ledger no longer writes it — the path is the
// default source for the one-shot legacy import (see Store.ImportLegacy).
//
// An absolute $XDG_CACHE_HOME is honoured; otherwise the location stays
// under os.UserCacheDir() — the historical default for this store, kept
// so an existing savings file is not orphaned. Returns an empty string
// when no cache dir can be resolved.
func DefaultPath() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v == "" || !filepath.IsAbs(v) {
		if base, err := os.UserCacheDir(); err != nil || base == "" {
			return ""
		}
	}
	return filepath.Join(platform.OSCacheDir(), "savings.json")
}

// DefaultEventsPath returns the flat-file era's savings.jsonl event-log
// path next to DefaultPath. Empty when the cache dir is unavailable.
func DefaultEventsPath() string {
	p := DefaultPath()
	if p == "" {
		return ""
	}
	return EventsPathFor(p)
}

// EventsPathFor returns the JSONL event-log path that corresponds to a
// flat-file cumulative savings JSON path — `<dir>/savings.jsonl` alongside
// the JSON file. Empty when storePath is empty.
func EventsPathFor(storePath string) string {
	if storePath == "" {
		return ""
	}
	dir := filepath.Dir(storePath)
	base := filepath.Base(storePath)
	ext := filepath.Ext(base)
	stem := base
	if ext != "" {
		stem = base[:len(base)-len(ext)]
	}
	return filepath.Join(dir, stem+".jsonl")
}

// Open opens the savings ledger inside the sidecar database at dbPath
// (creating tables as needed). An empty dbPath yields an in-memory-only
// store. The sidecar handle is process-shared: opening the same path the
// notes/memories managers use reuses their connection.
func Open(dbPath string) (*Store, error) {
	s := &Store{}
	s.mem = emptyFile()
	if dbPath == "" {
		return s, nil
	}
	sc, err := persistence.OpenSidecar(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open savings ledger: %w", err)
	}
	s.sc = sc
	return s, nil
}

func emptyFile() File {
	return File{
		Version:     schemaVersion,
		PerRepo:     make(map[string]*Totals),
		PerLanguage: make(map[string]*Totals),
	}
}

// AddObservation books one source-reading tool call. Durable immediately
// when the store is sidecar-backed; in-memory otherwise.
func (s *Store) AddObservation(o Observation) {
	if s == nil {
		return
	}
	if o.Saved < 0 {
		o.Saved = 0
	}
	now := time.Now().UTC()

	if s.sc != nil {
		_ = s.sc.AddSavingsObservation(persistence.SavingsEvent{
			TS:        now,
			SessionID: o.SessionID,
			Tool:      o.Tool,
			Repo:      o.Repo,
			Language:  o.Language,
			Returned:  o.Returned,
			Saved:     o.Saved,
		})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mem.FirstSeen.IsZero() {
		s.mem.FirstSeen = now
	}
	s.mem.LastUpdated = now
	s.mem.Totals.TokensSaved += o.Saved
	s.mem.Totals.TokensReturned += o.Returned
	s.mem.Totals.CallsCounted++
	addBucket := func(bucket map[string]*Totals, key string) {
		if key == "" {
			return
		}
		t := bucket[key]
		if t == nil {
			t = &Totals{}
			bucket[key] = t
		}
		t.TokensSaved += o.Saved
		t.TokensReturned += o.Returned
		t.CallsCounted++
	}
	addBucket(s.mem.PerRepo, o.Repo)
	addBucket(s.mem.PerLanguage, o.Language)
	s.memEvents = append(s.memEvents, Event{
		TS:        now,
		SessionID: o.SessionID,
		Repo:      o.Repo,
		Language:  o.Language,
		Tool:      o.Tool,
		Returned:  o.Returned,
		Saved:     o.Saved,
	})
}

// Snapshot returns the current cumulative totals. Sidecar-backed stores
// read the live aggregates, so the snapshot reflects every writer process,
// not just this one. FirstSeen stays the zero time until something has
// actually been recorded — callers must not present it as "tracking since"
// when it is zero.
func (s *Store) Snapshot() File {
	if s == nil {
		return emptyFile()
	}
	if s.sc == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		cp := s.mem
		cp.PerRepo = copyTotalsMap(s.mem.PerRepo)
		cp.PerLanguage = copyTotalsMap(s.mem.PerLanguage)
		return cp
	}

	buckets, firstSeen, lastUpdated, err := s.sc.SavingsTotals()
	if err != nil {
		return emptyFile()
	}
	out := emptyFile()
	out.FirstSeen = firstSeen
	out.LastUpdated = lastUpdated
	for bucket, r := range buckets {
		t := &Totals{TokensSaved: r.Saved, TokensReturned: r.Returned, CallsCounted: r.Calls}
		switch {
		case bucket == "":
			out.Totals = *t
		case len(bucket) > 5 && bucket[:5] == "repo:":
			out.PerRepo[bucket[5:]] = t
		case len(bucket) > 5 && bucket[:5] == "lang:":
			out.PerLanguage[bucket[5:]] = t
		}
	}
	return out
}

// EventsSince returns the per-call events with TS >= since, oldest first.
// since=zero returns the full event history.
func (s *Store) EventsSince(since time.Time) ([]Event, error) {
	if s == nil {
		return nil, nil
	}
	if s.sc == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return FilterSince(s.memEvents, since), nil
	}
	rows, err := s.sc.SavingsEventsSince(since)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, Event{
			TS:        r.TS,
			SessionID: r.SessionID,
			Repo:      r.Repo,
			Language:  r.Language,
			Tool:      r.Tool,
			Returned:  r.Returned,
			Saved:     r.Saved,
		})
	}
	return out, nil
}

// Flush is a no-op kept for call-site compatibility: every observation
// commits durably as it is recorded, so there is nothing to flush.
func (s *Store) Flush() error { return nil }

// Reset wipes all cumulative data and events. Used by
// `gortex savings --reset`. The legacy-import mark survives, so flat
// files already imported (and renamed *.bak) are not re-imported.
func (s *Store) Reset() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.mem = emptyFile()
	s.memEvents = nil
	s.mu.Unlock()
	if s.sc == nil {
		return nil
	}
	return s.sc.ResetSavings()
}

// ImportLegacy imports the flat-file era's ledger — the cumulative
// savings.json at jsonPath and its sibling savings.jsonl — into the
// sidecar, then renames both files to *.bak. Idempotent: a migration
// mark guarantees the import runs once per sidecar, including when
// there was nothing to import. In-memory stores skip the import.
func (s *Store) ImportLegacy(jsonPath string) error {
	if s == nil || s.sc == nil || jsonPath == "" {
		return nil
	}
	if s.sc.SavingsLegacyImportDone() {
		return nil
	}

	legacy, err := readLegacyFile(jsonPath)
	if err != nil {
		return err
	}
	eventsPath := EventsPathFor(jsonPath)
	events, _ := LoadEvents(eventsPath, time.Time{})

	buckets := make(map[string]persistence.SavingsTotalsRow)
	var firstSeen, lastUpdated time.Time
	switch {
	case legacy != nil:
		buckets[""] = totalsRow(legacy.Totals)
		for k, v := range legacy.PerRepo {
			buckets["repo:"+k] = totalsRow(*v)
		}
		for k, v := range legacy.PerLanguage {
			buckets["lang:"+k] = totalsRow(*v)
		}
		firstSeen, lastUpdated = legacy.FirstSeen, legacy.LastUpdated
	case len(events) > 0:
		// Event log without a cumulative file (e.g. the cumulative
		// flush never ran before the process died): rebuild the
		// totals the file would have carried.
		for _, ev := range events {
			bump := func(bucket string) {
				r := buckets[bucket]
				r.Saved += ev.Saved
				r.Returned += ev.Returned
				r.Calls++
				buckets[bucket] = r
			}
			bump("")
			if ev.Repo != "" {
				bump("repo:" + ev.Repo)
			}
			if ev.Language != "" {
				bump("lang:" + ev.Language)
			}
		}
		firstSeen = events[0].TS
		lastUpdated = events[len(events)-1].TS
	}

	pevents := make([]persistence.SavingsEvent, 0, len(events))
	for _, ev := range events {
		pevents = append(pevents, persistence.SavingsEvent{
			TS:        ev.TS,
			SessionID: ev.SessionID,
			Tool:      ev.Tool,
			Repo:      ev.Repo,
			Language:  ev.Language,
			Returned:  ev.Returned,
			Saved:     ev.Saved,
		})
	}
	if err := s.sc.ImportLegacySavings(buckets, firstSeen, lastUpdated, pevents); err != nil {
		return err
	}
	renameLegacySavings(jsonPath)
	renameLegacySavings(eventsPath)
	return nil
}

func totalsRow(t Totals) persistence.SavingsTotalsRow {
	return persistence.SavingsTotalsRow{Saved: t.TokensSaved, Returned: t.TokensReturned, Calls: t.CallsCounted}
}

// renameLegacySavings moves an already-imported flat file aside to
// <file>.bak. Best-effort — never deletes; a missing file is fine.
func renameLegacySavings(path string) {
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = os.Rename(path, path+".bak")
}

// readLegacyFile loads a flat-file era savings.json. Returns (nil, nil)
// when the file doesn't exist; corrupt or version-mismatched files are
// skipped the same way (the import has nothing trustworthy to carry over).
func readLegacyFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy savings: %w", err)
	}
	var loaded File
	if jerr := json.Unmarshal(data, &loaded); jerr != nil || loaded.Version != schemaVersion {
		return nil, nil
	}
	if loaded.PerRepo == nil {
		loaded.PerRepo = make(map[string]*Totals)
	}
	if loaded.PerLanguage == nil {
		loaded.PerLanguage = make(map[string]*Totals)
	}
	return &loaded, nil
}

// copyTotalsMap returns a deep copy of a name → Totals map.
func copyTotalsMap(src map[string]*Totals) map[string]*Totals {
	if src == nil {
		return make(map[string]*Totals)
	}
	dst := make(map[string]*Totals, len(src))
	for k, v := range src {
		cp := *v
		dst[k] = &cp
	}
	return dst
}
