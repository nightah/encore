//go:build retrieverbench

package match

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/nightah/encore/internal/core/media"
)

// sqliteRetriever is an experimental retriever on SQLite FTS5 (modernc.org/sqlite, in memory): a unicode61 table for words and phonetic keys ranked by bm25, and a trigram-tokenized table as the fallback. FTS5 has no fuzzy matching, so fuzzy words are expanded from the fts5vocab term list with the same edit-distance rules bleve uses.
type sqliteRetriever struct {
	lang *language
	db   *sql.DB
}

var sqliteSeq atomic.Int64

func init() {
	scaleRetrievers["sqlite-fts5"] = func(l *language) (retriever, error) { return newSQLiteRetriever(l) }
}

func newSQLiteRetriever(l *language) (*sqliteRetriever, error) {
	// A named shared-cache memory database lets the read pool see one copy.
	dsn := fmt.Sprintf("file:fts%d?mode=memory&cache=shared", sqliteSeq.Add(1))

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(8)

	for _, stmt := range []string{
		`CREATE TABLE docs (slot INTEGER PRIMARY KEY, id TEXT NOT NULL UNIQUE, kind TEXT NOT NULL)`,
		`CREATE VIRTUAL TABLE w USING fts5(words, phon, content='', contentless_delete=1, tokenize="unicode61 remove_diacritics 0")`,
		`CREATE VIRTUAL TABLE wv USING fts5vocab(w, 'row')`,
		`CREATE VIRTUAL TABLE g USING fts5(compact, content='', contentless_delete=1, tokenize='trigram')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("%s: %w", stmt, err)
		}
	}

	return &sqliteRetriever{lang: l, db: db}, nil
}

func (r *sqliteRetriever) upsert(entries []*entry) error {
	ctx := context.Background()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer tx.Rollback()

	if err := r.deleteIn(tx, idsOf(entries)); err != nil {
		return err
	}

	insDoc, err := tx.Prepare(`INSERT INTO docs (id, kind) VALUES (?, ?)`)
	if err != nil {
		return err
	}

	insW, err := tx.Prepare(`INSERT INTO w (rowid, words, phon) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}

	insG, err := tx.Prepare(`INSERT INTO g (rowid, compact) VALUES (?, ?)`)
	if err != nil {
		return err
	}

	for _, e := range entries {
		res, err := insDoc.Exec(e.doc.ID, string(e.doc.Kind))
		if err != nil {
			return err
		}

		slot, _ := res.LastInsertId()
		d := bleveDoc(e)

		var phon []string
		for _, s := range d[fieldPhon].([]string) { //nolint:forcetypeassert // bleveDoc's shape.
			phon = append(phon, r.lang.phonetic(s)...)
		}

		if _, err := insW.Exec(slot, d[fieldNorm], strings.Join(phon, " ")); err != nil {
			return err
		}

		if _, err := insG.Exec(slot, strings.Join(d[fieldNgram].([]string), " ")); err != nil { //nolint:forcetypeassert // bleveDoc's shape.
			return err
		}
	}

	return tx.Commit()
}

func idsOf(entries []*entry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.doc.ID
	}

	return ids
}

func (r *sqliteRetriever) deleteIn(tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		var slot int64

		err := tx.QueryRow(`SELECT slot FROM docs WHERE id = ?`, id).Scan(&slot)
		if err == sql.ErrNoRows {
			continue
		}

		if err != nil {
			return err
		}

		for _, stmt := range []string{`DELETE FROM w WHERE rowid = ?`, `DELETE FROM g WHERE rowid = ?`, `DELETE FROM docs WHERE slot = ?`} {
			if _, err := tx.Exec(stmt, slot); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *sqliteRetriever) remove(ids []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	if err := r.deleteIn(tx, ids); err != nil {
		return err
	}

	return tx.Commit()
}

func fts5Quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (r *sqliteRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	terms := r.lang.queryTokens(q)

	var words []string

	for _, t := range terms {
		words = append(words, fts5Quote(t))

		fz := fuzziness(t)
		if fz == 0 {
			continue
		}

		rt := []rune(t)
		lo := string(rt[0])
		hi := string(rt[0] + 1)

		rows, err := r.db.QueryContext(ctx, `SELECT term FROM wv WHERE term >= ? AND term < ?`, lo, hi)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var term string
			if err := rows.Scan(&term); err != nil {
				rows.Close()

				return nil, err
			}

			if n := utf8.RuneCountInString(term); n < len(rt)-fz || n > len(rt)+fz || term == t {
				continue
			}

			if osaWithin(rt, term, fz) >= 0 {
				words = append(words, fts5Quote(term))
			}
		}

		rows.Close()
	}

	var keys []string

	for i, t := range q.toks {
		if slices.Contains(terms, t) {
			for _, k := range q.keys[i] {
				keys = append(keys, fts5Quote(strings.ToLower(k)))
			}
		}
	}

	for _, k := range q.compactKeys {
		keys = append(keys, fts5Quote(strings.ToLower(k)))
	}

	var parts []string
	if len(words) > 0 {
		parts = append(parts, "words : ("+strings.Join(words, " OR ")+")")
	}

	if len(keys) > 0 {
		parts = append(parts, "phon : ("+strings.Join(keys, " OR ")+")")
	}

	kindSQL, args := "", []any{}
	if len(kinds) > 0 {
		kindSQL = " AND d.kind IN (" + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + ")"
		for _, kd := range kinds {
			args = append(args, string(kd))
		}
	}

	var ids []string

	if len(parts) > 0 {
		var err error

		ids, err = r.query(ctx, `SELECT d.id FROM w JOIN docs d ON d.slot = w.rowid WHERE w MATCH ?`+kindSQL+` ORDER BY bm25(w, 3.0, 2.0) LIMIT ?`, strings.Join(parts, " OR "), args, k)
		if err != nil {
			return nil, err
		}
	}

	if len(ids) >= min(k, ngramFallbackHits) {
		return ids, nil
	}

	var grams []string
	for _, g := range appendTrigrams(nil, q.compact) {
		if !slices.Contains(grams, fts5Quote(g)) {
			grams = append(grams, fts5Quote(g))
		}
	}

	if len(grams) == 0 {
		return ids, nil
	}

	more, err := r.query(ctx, `SELECT d.id FROM g JOIN docs d ON d.slot = g.rowid WHERE g MATCH ?`+kindSQL+` ORDER BY bm25(g) LIMIT ?`, strings.Join(grams, " OR "), args, k)
	if err != nil {
		return nil, err
	}

	for _, id := range more {
		if len(ids) >= k {
			break
		}

		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

func (r *sqliteRetriever) query(ctx context.Context, stmt, match string, kindArgs []any, k int) ([]string, error) {
	args := append([]any{match}, kindArgs...)
	args = append(args, k)

	rows, err := r.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("%s [%s]: %w", stmt, match, err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (r *sqliteRetriever) close() error { return r.db.Close() }
