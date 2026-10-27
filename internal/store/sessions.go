package store

import (
	"context"
	"fmt"
	"time"
)

// Sessions returns the session repository.
func (db *DB) Sessions() Sessions { return Sessions{db} }

func scanSession(s scanner) (Session, error) {
	var (
		sess                   Session
		created, expires, seen int64
	)

	if err := s.Scan(&sess.IDHash, &sess.Username, &created, &expires, &seen); err != nil {
		return Session{}, mapErr(err)
	}

	sess.CreatedAt, sess.ExpiresAt, sess.LastSeenAt = fromMillis(created), fromMillis(expires), fromMillis(seen)

	return sess, nil
}

// Create stores a new session; CreatedAt and LastSeenAt are set to now.
func (r Sessions) Create(ctx context.Context, s *Session) error {
	now := r.db.stamp()
	s.CreatedAt, s.LastSeenAt = now, now
	s.ExpiresAt = s.ExpiresAt.UTC().Truncate(time.Millisecond)

	_, err := r.db.write.ExecContext(ctx, `INSERT INTO sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?)`,
		s.IDHash, s.Username, toMillis(now), toMillis(s.ExpiresAt), toMillis(now))
	if err != nil {
		return fmt.Errorf("creating session: %w", mapErr(err))
	}

	return nil
}

// Get returns an unexpired session, or ErrNotFound.
func (r Sessions) Get(ctx context.Context, idHash []byte) (Session, error) {
	s, err := scanSession(r.db.read.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		idHash, toMillis(r.db.now())))
	if err != nil {
		return Session{}, fmt.Errorf("getting session: %w", err)
	}

	return s, nil
}

// Touch records activity on a session and moves its expiry, for sliding sessions.
func (r Sessions) Touch(ctx context.Context, idHash []byte, expiresAt time.Time) error {
	err := affected(r.db.write.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id_hash = ?`,
		toMillis(r.db.now()), toMillis(expiresAt), idHash))
	if err != nil {
		return fmt.Errorf("touching session: %w", err)
	}

	return nil
}

// Delete ends one session. Deleting a missing session is not an error.
func (r Sessions) Delete(ctx context.Context, idHash []byte) error {
	if _, err := r.db.write.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

// DeleteUser ends every session of a user and returns how many were removed.
func (r Sessions) DeleteUser(ctx context.Context, username string) (int64, error) {
	res, err := r.db.write.ExecContext(ctx, `DELETE FROM sessions WHERE username = ?`, username)
	if err != nil {
		return 0, fmt.Errorf("deleting sessions of %q: %w", username, err)
	}

	return res.RowsAffected()
}

// DeleteAllExcept ends every session except the one whose hash is keep (nil keeps none) and returns how many were removed, e.g. after the admin password or sign-in mode changed.
func (r Sessions) DeleteAllExcept(ctx context.Context, keep []byte) (int64, error) {
	res, err := r.db.write.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash IS NOT ?`, keep)
	if err != nil {
		return 0, fmt.Errorf("deleting sessions: %w", err)
	}

	return res.RowsAffected()
}

// DeleteExpired removes expired sessions and returns how many were removed.
func (r Sessions) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.write.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, toMillis(r.db.now()))
	if err != nil {
		return 0, fmt.Errorf("deleting expired sessions: %w", err)
	}

	return res.RowsAffected()
}
