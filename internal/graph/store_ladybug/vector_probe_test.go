//go:build ladybug

package store_ladybug

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVector_Probe mirrors fts_probe_test.go for the vector
// extension. Confirms the CALL syntax and the auto-update
// semantics the production wiring will rely on:
//
//  1. INSTALL VECTOR + LOAD EXTENSION VECTOR (matches the FTS dance)
//  2. CREATE NODE TABLE with a FLOAT[N] column for the embedding
//  3. CALL CREATE_VECTOR_INDEX(table, name, column[, metric])
//  4. CALL QUERY_VECTOR_INDEX(table, name, queryVec, k) — find signature
//  5. Auto-update on later AddNode
//
// Liberal logging (instead of strict assertions) so the probe
// surfaces what works regardless of where Ladybug 0.13 lands on
// the syntax-versioning curve — we'll then encode the discovered
// shape into production.
func TestVector_Probe(t *testing.T) {
	dir, err := os.MkdirTemp("", "lbug-vec-probe-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	s, err := Open(filepath.Join(dir, "store.lbug"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Step 1: install + load the vector extension. Mirrors the FTS
	// dance — Ladybug ships the extension compiled in but requires
	// explicit load before the CREATE_VECTOR_INDEX function appears
	// in the catalog.
	for _, q := range []string{`INSTALL VECTOR`, `LOAD EXTENSION VECTOR`} {
		if err := tryRunCypher(s, q); err != nil {
			t.Logf("%s: %v", q, err)
		} else {
			t.Logf("%s: ok", q)
		}
	}

	// Step 2: probe FLOAT[N] column support. Try the spec-style
	// `FLOAT[4]` first, fall back to `ARRAY[FLOAT,4]` if needed.
	for _, ddl := range []string{
		`CREATE NODE TABLE IF NOT EXISTS VecProbe(id STRING, emb FLOAT[4], PRIMARY KEY(id))`,
		`CREATE NODE TABLE IF NOT EXISTS VecProbe2(id STRING, emb ARRAY[FLOAT,4], PRIMARY KEY(id))`,
	} {
		if err := tryRunCypher(s, ddl); err != nil {
			t.Logf("CREATE %q: %v", ddl, err)
		} else {
			t.Logf("CREATE %q: ok", ddl)
		}
	}

	// Step 3: seed a few rows so the index has something to build over.
	for i, vec := range [][]float32{
		{1.0, 0.0, 0.0, 0.0},
		{0.9, 0.1, 0.0, 0.0},
		{0.0, 0.0, 0.0, 1.0},
	} {
		id := []string{"alpha", "alpha_neighbor", "far"}[i]
		err := tryRunCypherArgs(s, `MERGE (n:VecProbe {id: $id}) SET n.emb = $emb`, map[string]any{
			"id":  id,
			"emb": vec,
		})
		if err != nil {
			t.Logf("insert %s: %v", id, err)
		}
	}

	// Step 4: try every CREATE_VECTOR_INDEX shape we know of.
	for _, ddl := range []string{
		`CALL CREATE_VECTOR_INDEX('VecProbe', 'idx_emb_v', 'emb')`,
		`CALL CREATE_VECTOR_INDEX('VecProbe', 'idx_emb_v', 'emb', 'cosine')`,
		`CALL CREATE_VECTOR_INDEX('VecProbe', 'idx_emb_v', 'emb', 4, 'cosine')`,
	} {
		if err := tryRunCypher(s, ddl); err != nil {
			t.Logf("CREATE_VECTOR_INDEX %q: %v", ddl, err)
		} else {
			t.Logf("CREATE_VECTOR_INDEX %q: ok", ddl)
			break
		}
	}

	// Step 5: try QUERY_VECTOR_INDEX with both 3-arg and 4-arg shapes.
	for _, probe := range []struct {
		q    string
		args map[string]any
	}{
		{`CALL QUERY_VECTOR_INDEX('VecProbe', 'idx_emb_v', $vec, 5) RETURN node.id, distance`,
			map[string]any{"vec": []float32{1.0, 0.0, 0.0, 0.0}}},
		{`CALL QUERY_VECTOR_INDEX('VecProbe', 'idx_emb_v', $vec) RETURN node.id, distance LIMIT 5`,
			map[string]any{"vec": []float32{1.0, 0.0, 0.0, 0.0}}},
	} {
		rows, err := tryQueryCypher(s, probe.q, probe.args)
		if err != nil {
			t.Logf("QUERY_VECTOR_INDEX %q: %v", probe.q, err)
			continue
		}
		t.Logf("QUERY_VECTOR_INDEX %q → %d rows", probe.q, len(rows))
		for _, r := range rows {
			t.Logf("  %v", r)
		}
	}
}

// tryRunCypherArgs invokes runWriteLocked with parameters, capturing
// any panic the binding raises (extension-not-loaded, wrong-types,
// etc.) as a normal Go error so the probe can react.
func tryRunCypherArgs(s *Store, q string, args map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recoverErr(r)
		}
	}()
	s.runWriteLocked(q, args)
	return nil
}
