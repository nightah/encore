package store

import (
	"context"
	"fmt"

	"github.com/nightah/encore/internal/core/media"
)

// Aliases returns the title alias repository.
func (db *DB) Aliases() Aliases { return Aliases{db} }

func scanAlias(s scanner) (TitleAlias, error) {
	var (
		a       TitleAlias
		created int64
	)

	if err := s.Scan(&a.ID, &a.KodiInstanceID, &a.Media.Kind, &a.Media.ID, &a.Alias, &a.Normalised, &created); err != nil {
		return TitleAlias{}, mapErr(err)
	}

	a.CreatedAt = fromMillis(created)

	return a, nil
}

// Add inserts a and sets its ID, normalised form and creation time. The same alias may point at several items; adding an identical alias twice returns ErrConflict.
func (r Aliases) Add(ctx context.Context, a *TitleAlias) error {
	a.Normalised = Normalise(a.Alias)
	a.CreatedAt = r.db.stamp()

	err := r.db.write.QueryRowContext(ctx, `INSERT INTO aliases (kodi_instance_id, media_kind, media_id, alias, normalised, created_at)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		a.KodiInstanceID, a.Media.Kind, a.Media.ID, a.Alias, a.Normalised, toMillis(a.CreatedAt)).Scan(&a.ID)
	if err != nil {
		return fmt.Errorf("adding alias “%s”: %w", a.Alias, mapErr(err))
	}

	return nil
}

// Delete removes an alias by ID.
func (r Aliases) Delete(ctx context.Context, id int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM aliases WHERE id = ?`, id)); err != nil {
		return fmt.Errorf("deleting alias %d: %w", id, err)
	}

	return nil
}

// List returns every alias for a Kodi instance.
func (r Aliases) List(ctx context.Context, kodiInstanceID int64) ([]TitleAlias, error) {
	return queryAll(ctx, r.db.read, scanAlias,
		`SELECT `+aliasColumns+` FROM aliases WHERE kodi_instance_id = ? ORDER BY normalised, id`, kodiInstanceID)
}

// ForMedia returns the aliases of one library item.
func (r Aliases) ForMedia(ctx context.Context, kodiInstanceID int64, ref media.Ref) ([]TitleAlias, error) {
	return queryAll(ctx, r.db.read, scanAlias,
		`SELECT `+aliasColumns+` FROM aliases WHERE kodi_instance_id = ? AND media_kind = ? AND media_id = ? ORDER BY normalised`,
		kodiInstanceID, ref.Kind, ref.ID)
}

// Lookup returns the aliases on an instance whose normalised text equals spoken's.
func (r Aliases) Lookup(ctx context.Context, kodiInstanceID int64, spoken string) ([]TitleAlias, error) {
	return queryAll(ctx, r.db.read, scanAlias,
		`SELECT `+aliasColumns+` FROM aliases WHERE kodi_instance_id = ? AND normalised = ? ORDER BY id`,
		kodiInstanceID, Normalise(spoken))
}
