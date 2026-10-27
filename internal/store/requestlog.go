package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (r RequestLogRetention) withDefaults() RequestLogRetention {
	if r.MaxEntries <= 0 {
		r.MaxEntries = DefaultRequestLogMaxEntries
	}

	if r.MaxAge <= 0 {
		r.MaxAge = DefaultRequestLogMaxAge
	}

	return r
}

// RequestLog returns the request log repository.
func (db *DB) RequestLog() RequestLog { return RequestLog{db} }

func scanRequestLog(s scanner) (RequestLogEntry, error) {
	var (
		e                       RequestLogEntry
		ts, durMS               int64
		slots, match, kodiCalls sql.NullString
		timing                  sql.NullString
	)

	err := s.Scan(&e.ID, &ts, &e.RequestID, &e.DeviceID, &e.Locale, &e.Intent, &slots, &match, &kodiCalls, &timing, &e.Speech, &e.Outcome, &durMS, &e.Simulated)
	if err != nil {
		return RequestLogEntry{}, mapErr(err)
	}

	e.Time = fromMillis(ts)
	e.Duration = time.Duration(durMS) * time.Millisecond
	e.Slots, e.Match, e.KodiCalls, e.Timing = rawOrNil(slots), rawOrNil(match), rawOrNil(kodiCalls), rawOrNil(timing)

	return e, nil
}

func rawOrNil(s sql.NullString) json.RawMessage {
	if !s.Valid {
		return nil
	}

	return json.RawMessage(s.String)
}

func nullJSON(raw json.RawMessage) sql.NullString {
	return sql.NullString{String: string(raw), Valid: len(raw) > 0}
}

// Insert appends e, setting its ID (and Time if zero), prunes entries beyond the retention limits, then notifies subscribers.
func (r RequestLog) Insert(ctx context.Context, e *RequestLogEntry) error {
	if e.Time.IsZero() {
		e.Time = r.db.stamp()
	} else {
		e.Time = e.Time.UTC().Truncate(time.Millisecond)
	}

	err := r.db.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `INSERT INTO request_log (ts, request_id, device_id, locale, intent, slots, match_info, kodi_calls, timing, speech, outcome, duration_ms, simulated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			toMillis(e.Time), e.RequestID, e.DeviceID, e.Locale, e.Intent, nullJSON(e.Slots), nullJSON(e.Match), nullJSON(e.KodiCalls), nullJSON(e.Timing),
			e.Speech, e.Outcome, e.Duration.Milliseconds(), e.Simulated).Scan(&e.ID)
		if err != nil {
			return mapErr(err)
		}

		return r.prune(ctx, tx)
	})
	if err != nil {
		return fmt.Errorf("logging request: %w", err)
	}

	r.db.subs.publish(*e)

	return nil
}

// Prune applies the retention limits now. Insert already prunes; this is for a periodic sweep so old entries age out on an idle server.
func (r RequestLog) Prune(ctx context.Context) error {
	return r.db.tx(ctx, func(tx *sql.Tx) error { return r.prune(ctx, tx) })
}

func (r RequestLog) prune(ctx context.Context, tx *sql.Tx) error {
	cutoff := toMillis(r.db.now().Add(-r.db.retention.MaxAge))

	_, err := tx.ExecContext(ctx, `DELETE FROM request_log WHERE ts < ?1
		OR id <= (SELECT id FROM request_log ORDER BY id DESC LIMIT 1 OFFSET ?2)`, cutoff, r.db.retention.MaxEntries)
	if err != nil {
		return fmt.Errorf("pruning request log: %w", err)
	}

	return nil
}

// List returns entries newest first.
func (r RequestLog) List(ctx context.Context, q RequestLogQuery) ([]RequestLogEntry, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}

	before := q.BeforeID
	if before <= 0 {
		before = 1<<63 - 1
	}

	return queryAll(ctx, r.db.read, scanRequestLog,
		`SELECT `+requestLogColumns+` FROM request_log WHERE id < ? ORDER BY id DESC LIMIT ?`, before, q.Limit)
}

// Get returns one entry.
func (r RequestLog) Get(ctx context.Context, id int64) (RequestLogEntry, error) {
	e, err := scanRequestLog(r.db.read.QueryRowContext(ctx, `SELECT `+requestLogColumns+` FROM request_log WHERE id = ?`, id))
	if err != nil {
		return RequestLogEntry{}, fmt.Errorf("getting request log entry %d: %w", id, err)
	}

	return e, nil
}

// Subscribe registers fn to be called with every entry after it is committed, until the returned cancel function is called. fn runs on the inserting goroutine, so it must not block: hand off to a buffered channel and drop when full.
func (r RequestLog) Subscribe(fn func(RequestLogEntry)) (cancel func()) {
	return r.db.subs.add(fn)
}

func (s *subscribers) add(fn func(RequestLogEntry)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fns == nil {
		s.fns = map[int]func(RequestLogEntry){}
	}

	id := s.next
	s.next++
	s.fns[id] = fn

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		delete(s.fns, id)
	}
}

func (s *subscribers) publish(e RequestLogEntry) {
	// Copy so a callback may cancel its own subscription without deadlocking.
	s.mu.RLock()
	fns := make([]func(RequestLogEntry), 0, len(s.fns))

	for _, fn := range s.fns {
		fns = append(fns, fn)
	}
	s.mu.RUnlock()

	for _, fn := range fns {
		fn(e)
	}
}
