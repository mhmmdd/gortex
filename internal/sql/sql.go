// Package sql parses SQL string literals into the table references
// they touch. Used by language extractors that detect calls into a
// SQL exec API (db.Query, db.Exec, sqlx.NamedExec, etc.) with a
// string-literal first arg — the literal goes through ExtractTables
// to get the names; the caller emits KindTable nodes plus EdgeQueries
// edges.
//
// Scope (v1): regex-based table extraction from FROM / JOIN /
// INSERT INTO / UPDATE / DELETE FROM clauses. The regex picks up
// the canonical patterns without spinning up a full SQL parser.
// Trade-offs:
//
//   - Dynamic SQL built by string concatenation or query builders
//     is invisible. Agents who care about that will fall back to
//     grep — same v1 stance the broader spec takes for noisy
//     extractions.
//
//   - Quoted identifiers (`"foo"`, `[foo]`) and case-sensitive
//     schema-qualified names (`schema.table`) are handled — the
//     regex strips quoting and keeps the trailing identifier, with
//     schema preserved in the meta when present.
//
//   - SQL keywords used as identifiers (`FROM "from"`) misclassify
//     as the keyword. A future enhancement could feed the regex
//     output through a SQL keyword list to filter them; v1 accepts
//     the noise.
//
//   - Default-off via the `sql` coverage gate per the spec — string-
//     literal SQL is noisy enough that opt-in is the right shape.
package sql

import (
	"regexp"
	"sort"
	"strings"
)

// tableRefPatterns enumerates the SQL clauses that introduce a
// table reference. Each pattern uses a single capture group on the
// table identifier. Case-insensitive match — SQL conventionally
// uppercases keywords but we tolerate either form.
var tableRefPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bFROM\s+([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
	regexp.MustCompile(`(?i)\bJOIN\s+([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
	regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
	regexp.MustCompile(`(?i)\bUPDATE\s+([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
	regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
	regexp.MustCompile(`(?i)\bTRUNCATE\s+(?:TABLE\s+)?([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`),
}

// TableRef is a single resolved table reference.
type TableRef struct {
	Table  string // unquoted table name (last segment if schema.table)
	Schema string // optional schema prefix; "" when none
	Op     string // canonical operation: select, insert, update, delete, truncate
}

// canonicalOp maps a clause keyword to a stable operation tag for
// downstream queries that scope by op (e.g. "find every site that
// truncates X").
func canonicalOp(clauseHead string) string {
	switch strings.ToUpper(strings.Fields(clauseHead)[0]) {
	case "FROM", "JOIN":
		return "select"
	case "INSERT":
		return "insert"
	case "UPDATE":
		return "update"
	case "DELETE":
		return "delete"
	case "TRUNCATE":
		return "truncate"
	}
	return ""
}

// ExtractTables walks query and returns the de-duplicated set of
// table references found. Order follows source-text occurrence so
// the result is diff-able across runs of the same query string.
func ExtractTables(query string) []TableRef {
	if query == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var refs []TableRef

	// `DELETE FROM` matches both the DELETE FROM pattern (correct)
	// and the bare FROM pattern (wrong — we'd report the same
	// table as both a select and a delete). Process compound
	// keywords first, mask out their match ranges so the FROM
	// regex doesn't see them, then process the remaining ones.
	working := maskDeleteFromForFromPattern(query)

	for i, re := range tableRefPatterns {
		// The FROM pattern (index 0) sees the masked text; the
		// DELETE FROM pattern (index 4) sees the original to find
		// its own matches first.
		text := query
		if i == 0 {
			text = working
		}
		matches := re.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			schema, table := splitSchemaTable(stripQuoting(m[1]))
			if table == "" {
				continue
			}
			op := canonicalOp(m[0])
			key := op + "::" + schema + "::" + table
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, TableRef{
				Table:  table,
				Schema: schema,
				Op:     op,
			})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Op != refs[j].Op {
			return refs[i].Op < refs[j].Op
		}
		if refs[i].Schema != refs[j].Schema {
			return refs[i].Schema < refs[j].Schema
		}
		return refs[i].Table < refs[j].Table
	})
	return refs
}

// maskDeleteFromForFromPattern substitutes the FROM keyword in
// "DELETE FROM" with a non-keyword sentinel so the bare FROM
// regex doesn't double-match the same table reference. The
// sentinel `__GFOX_FROM__` won't appear in real SQL and is
// valid in the regex's character class so it gets ignored
// silently. The DELETE FROM pattern still operates on the
// original query string and finds its own match.
var deleteFromMaskRe = regexp.MustCompile(`(?i)\b(DELETE)\s+FROM\b`)

func maskDeleteFromForFromPattern(query string) string {
	return deleteFromMaskRe.ReplaceAllString(query, "$1 __GFOX_FROM__")
}

// stripQuoting removes the four shapes of SQL identifier quoting:
// double quotes (ANSI), backticks (MySQL), brackets (T-SQL). The
// inner content is returned unchanged otherwise.
func stripQuoting(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 {
		first, last := name[0], name[len(name)-1]
		switch {
		case first == '"' && last == '"',
			first == '`' && last == '`',
			first == '[' && last == ']':
			return name[1 : len(name)-1]
		}
	}
	return name
}

// splitSchemaTable separates `schema.table` into its parts.
// Multi-dot identifiers (`db.schema.table`) collapse to schema=
// "schema", table="table" — the leftmost segment is database-
// scoped and rarely useful for graph queries.
func splitSchemaTable(name string) (schema, table string) {
	if i := strings.LastIndex(name, "."); i >= 0 {
		schema = name[:i]
		table = name[i+1:]
		// If the schema piece itself has a database segment, keep
		// only the immediate parent.
		if j := strings.LastIndex(schema, "."); j >= 0 {
			schema = schema[j+1:]
		}
		return strings.TrimSpace(stripQuoting(schema)), strings.TrimSpace(stripQuoting(table))
	}
	return "", strings.TrimSpace(name)
}

// createTableRe matches CREATE TABLE [IF NOT EXISTS] declarations
// across the four canonical identifier-quoting styles. Used by
// ExtractCreateTables for migration-file extraction — distinct
// from ExtractTables, which scans query strings rather than DDL.
var createTableRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:GLOBAL\s+TEMPORARY\s+|LOCAL\s+TEMPORARY\s+|TEMPORARY\s+|TEMP\s+|UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_"\x60\[][a-zA-Z0-9_."\x60\]]*)`)

// ExtractCreateTables returns the tables declared by CREATE TABLE
// statements in a SQL source file. Schema-qualified names retain
// their schema in TableRef.Schema; identifier quoting is stripped.
// Op is always "create".
//
// Used by migration-file extraction where the SQL source is a DDL
// script rather than an embedded query string. Drop / alter
// statements are deliberately not extracted — a migration that
// drops a table doesn't *provide* the table to the rest of the
// repo, and modeling alter-as-delta would require maintaining
// per-migration ordering that's out of scope for the v1.
func ExtractCreateTables(source string) []TableRef {
	if source == "" {
		return nil
	}
	matches := createTableRe.FindAllStringSubmatch(source, -1)
	seen := make(map[string]struct{})
	var refs []TableRef
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		schema, table := splitSchemaTable(stripQuoting(m[1]))
		if table == "" {
			continue
		}
		key := schema + "::" + table
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, TableRef{
			Table:  table,
			Schema: schema,
			Op:     "create",
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Schema != refs[j].Schema {
			return refs[i].Schema < refs[j].Schema
		}
		return refs[i].Table < refs[j].Table
	})
	return refs
}

// MigrationNodeID is the canonical synthetic ID for a migration
// node. The path component lets agents reach the originating file
// in one step; the prefix matches the synthetic-ID convention
// used by db:: tables and module:: dependencies.
func MigrationNodeID(path string) string {
	return "migration::" + path
}

// IsMigrationPath returns true when filePath looks like a SQL
// migration file. Recognised conventions: any .sql file under a
// directory whose name contains "migrate" or "migration"
// (case-insensitive). Matches Rails (db/migrate/), golang-migrate
// (migrations/), Alembic when wrapped (we mostly handle alembic
// via Python parsers separately), and most ORM generators.
func IsMigrationPath(filePath string) bool {
	if !strings.HasSuffix(strings.ToLower(filePath), ".sql") {
		return false
	}
	lower := strings.ToLower(filePath)
	for _, segment := range []string{"/migrate/", "/migrations/", "/migrate.", "/migrations."} {
		if strings.Contains(lower, segment) {
			return true
		}
	}
	return strings.HasPrefix(lower, "migrate/") ||
		strings.HasPrefix(lower, "migrations/") ||
		strings.HasPrefix(lower, "db/migrate/") ||
		strings.HasPrefix(lower, "db/migrations/")
}

// TableNodeID returns the canonical synthetic ID for a table
// reference. Mirrors the ecosystem-prefix convention used by
// module:: / external:: / annotation:: / event:: nodes — `db::`
// keeps the table namespace distinct.
//
// dialect is the SQL dialect tag (postgres, mysql, sqlite,
// generic) — included on the ID so cross-dialect projects can
// distinguish a Postgres `users` table from a MySQL one in the
// same graph. The default dialect is "generic" when the caller
// doesn't know.
func TableNodeID(dialect, schema, table string) string {
	if dialect == "" {
		dialect = "generic"
	}
	prefix := "db::" + dialect + "::"
	if schema == "" {
		return prefix + table
	}
	return prefix + schema + "." + table
}
