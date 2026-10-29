package testkit

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

var faulty = struct {
	mu    sync.RWMutex
	paths map[string]*Faults
	next  atomic.Int64
}{paths: map[string]*Faults{}}

// Every connection the test binary opens gets the authorizer, so a database opened before NewFaults is called can still have reads fail.
func init() {
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		reg, ok := conn.(sqlite.AuthorizerRegisterer)
		if !ok {
			return nil
		}

		return reg.RegisterAuthorizer(authorizer(dsnPath(dsn)))
	})
}

// NewFaults returns the fault injector for the database file at path, which must already exist with its schema for write faults to be added. Faults last until Clear or the end of the test.
func NewFaults(t testing.TB, path string) *Faults {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("testkit: resolving %s: %v", path, err)
	}

	f := &Faults{t: t, path: abs}

	faulty.mu.Lock()
	defer faulty.mu.Unlock()

	if faulty.paths[abs] != nil {
		t.Fatalf("testkit: %s already has a fault injector", abs)
	}

	faulty.paths[abs] = f

	t.Cleanup(func() {
		faulty.mu.Lock()
		delete(faulty.paths, abs)
		faulty.mu.Unlock()
	})

	return f
}

// Inject adds faults to those already in place.
func (f *Faults) Inject(faults ...Fault) {
	f.t.Helper()

	for _, fault := range faults {
		nth := max(fault.Nth, 1)

		switch fault.Op {
		case Read:
			if fault.When != "" {
				f.t.Fatalf("testkit: a read fault cannot have a When condition")
			}

			f.mu.Lock()
			f.reads = append(f.reads, &readFault{table: fault.Table, nth: nth})
			f.mu.Unlock()
		case Insert, Update, Delete:
			f.addTrigger(fault, nth)
		default:
			f.t.Fatalf("testkit: unknown fault operation %q", fault.Op)
		}
	}
}

// Clear removes every fault, so the database works normally again.
func (f *Faults) Clear() {
	f.t.Helper()

	f.mu.Lock()
	f.reads = nil
	triggers := f.triggers
	f.triggers = nil
	f.mu.Unlock()

	if len(triggers) == 0 {
		return
	}

	stmts := make([]string, 0, len(triggers)+1)
	for _, name := range triggers {
		stmts = append(stmts, `DROP TRIGGER IF EXISTS `+name)
	}

	f.exec(append(stmts, `DELETE FROM `+faultPrefix)...)
}

func (f *Faults) addTrigger(fault Fault, nth int) {
	name := fmt.Sprintf("%s_%d", faultPrefix, faulty.next.Add(1))

	when := ""
	if fault.When != "" {
		when = "WHEN " + fault.When
	}

	f.exec(
		`CREATE TABLE IF NOT EXISTS `+faultPrefix+` (name TEXT PRIMARY KEY, seen INTEGER NOT NULL DEFAULT 0)`,
		fmt.Sprintf(`INSERT INTO %s (name) VALUES ('%s')`, faultPrefix, name),
		// RAISE(ABORT) undoes the failing statement, including its count, so the nth row fails and so does every later one.
		fmt.Sprintf(`CREATE TRIGGER %[1]s BEFORE %[2]s ON %[3]s %[4]s BEGIN
			UPDATE %[5]s SET seen = seen + 1 WHERE name = '%[1]s';
			SELECT RAISE(ABORT, '%[6]s on %[2]s %[3]s') WHERE (SELECT seen FROM %[5]s WHERE name = '%[1]s') >= %[7]d;
		END`, name, fault.Op, fault.Table, when, faultPrefix, FaultMessage, nth),
	)

	f.mu.Lock()
	f.triggers = append(f.triggers, name)
	f.mu.Unlock()
}

func (f *Faults) exec(stmts ...string) {
	f.t.Helper()

	u := url.URL{Scheme: "file", Path: f.path, RawQuery: url.Values{"_pragma": {"busy_timeout(5000)"}}.Encode()}

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		f.t.Fatalf("testkit: opening %s: %v", f.path, err)
	}
	defer db.Close()

	for _, stmt := range stmts {
		if _, err := db.ExecContext(f.t.Context(), stmt); err != nil {
			f.t.Fatalf("testkit: %s: %v", stmt, err)
		}
	}
}

// failRead counts a statement reading table and reports whether it should fail.
func (f *Faults) failRead(table string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	fail := false

	for _, r := range f.reads {
		if r.table == table {
			r.seen++
			fail = fail || r.seen >= r.nth
		}
	}

	return fail
}

// authorizer counts each statement once per table it selects from: SQLite presents a SELECT, then a read of each column it uses. Reads inside writes, such as foreign key checks, aren't counted, nor is anything the fault triggers do.
func authorizer(path string) sqlite.AuthorizerFn {
	var (
		selecting bool
		counted   map[string]bool
	)

	return func(action sqlite.AuthorizerActionCode, table, _, _, trigger string) sqlite.AuthorizerReturnCode {
		if strings.HasPrefix(trigger, faultPrefix) {
			return sqlite.AuthorizerOK
		}

		switch action {
		case sqlite.AuthSelect:
			selecting, counted = true, map[string]bool{}
		case sqlite.AuthRead:
			if !selecting || counted[table] {
				return sqlite.AuthorizerOK
			}

			counted[table] = true

			faulty.mu.RLock()
			f := faulty.paths[path]
			faulty.mu.RUnlock()

			if f != nil && f.failRead(table) {
				return sqlite.AuthorizerDeny
			}
		case sqlite.AuthFunction, sqlite.AuthRecursive:
			// Still part of the SELECT.
		default:
			selecting = false
		}

		return sqlite.AuthorizerOK
	}
}

func dsnPath(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "file" {
		return ""
	}

	return u.Path
}
