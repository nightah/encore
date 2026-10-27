package store

import (
	"context"
	"database/sql"
	"fmt"
)

// LibraryGroups returns the library group repository.
func (db *DB) LibraryGroups() LibraryGroups { return LibraryGroups{db} }

func scanLibraryGroup(s scanner) (LibraryGroup, error) {
	var (
		g                LibraryGroup
		source           sql.NullInt64
		evidence         sql.NullString
		created, updated int64
	)

	if err := s.Scan(&g.ID, &g.Name, &source, &g.State, &g.Manual, &evidence, &created, &updated); err != nil {
		return LibraryGroup{}, mapErr(err)
	}

	g.SourceInstanceID = source.Int64
	g.CreatedAt, g.UpdatedAt = fromMillis(created), fromMillis(updated)
	g.Evidence = rawOrNil(evidence)

	return g, nil
}

// Save creates g (when its ID is 0) or updates it, then makes members exactly the instances that belong to it, in one transaction. Instances that belonged to g but aren't listed are left without a group. A nil members leaves membership alone.
func (r LibraryGroups) Save(ctx context.Context, g *LibraryGroup, members []int64) error {
	now := r.db.stamp()

	if g.State == "" {
		g.State = GroupSolo
	}

	return r.db.tx(ctx, func(tx *sql.Tx) error {
		if g.ID == 0 {
			err := tx.QueryRowContext(ctx, `INSERT INTO library_groups (name, source_instance_id, state, manual, evidence, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
				g.Name, nullID(g.SourceInstanceID), g.State, g.Manual, nullJSON(g.Evidence), toMillis(now), toMillis(now)).Scan(&g.ID)
			if err != nil {
				return fmt.Errorf("creating library group: %w", mapErr(err))
			}

			g.CreatedAt = now
		} else {
			err := affected(tx.ExecContext(ctx, `UPDATE library_groups SET name = ?, source_instance_id = ?, state = ?, manual = ?, evidence = ?, updated_at = ? WHERE id = ?`,
				g.Name, nullID(g.SourceInstanceID), g.State, g.Manual, nullJSON(g.Evidence), toMillis(now), g.ID))
			if err != nil {
				return fmt.Errorf("updating library group %d: %w", g.ID, err)
			}
		}

		g.UpdatedAt = now

		if members == nil {
			return nil
		}

		if _, err := tx.ExecContext(ctx, `UPDATE kodi_instances SET library_group_id = NULL WHERE library_group_id = ?`, g.ID); err != nil {
			return fmt.Errorf("clearing members of library group %d: %w", g.ID, mapErr(err))
		}

		for _, id := range members {
			if err := affected(tx.ExecContext(ctx, `UPDATE kodi_instances SET library_group_id = ? WHERE id = ?`, g.ID, id)); err != nil {
				return fmt.Errorf("adding kodi instance %d to library group %d: %w", id, g.ID, err)
			}
		}

		return nil
	})
}

// Assign moves an instance into a group, or out of every group when groupID is 0.
func (r LibraryGroups) Assign(ctx context.Context, instanceID, groupID int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `UPDATE kodi_instances SET library_group_id = ? WHERE id = ?`, nullID(groupID), instanceID)); err != nil {
		return fmt.Errorf("assigning kodi instance %d to library group %d: %w", instanceID, groupID, err)
	}

	return nil
}

// Get returns the group with the given ID.
func (r LibraryGroups) Get(ctx context.Context, id int64) (LibraryGroup, error) {
	g, err := scanLibraryGroup(r.db.read.QueryRowContext(ctx, `SELECT `+libraryGroupColumns+` FROM library_groups WHERE id = ?`, id))
	if err != nil {
		return LibraryGroup{}, fmt.Errorf("getting library group %d: %w", id, err)
	}

	return g, nil
}

// List returns every group ordered by ID.
func (r LibraryGroups) List(ctx context.Context) ([]LibraryGroup, error) {
	return queryAll(ctx, r.db.read, scanLibraryGroup, `SELECT `+libraryGroupColumns+` FROM library_groups ORDER BY id`)
}

// Members returns the IDs of a group's instances, in ID order.
func (r LibraryGroups) Members(ctx context.Context, id int64) ([]int64, error) {
	return queryAll(ctx, r.db.read, scanID, `SELECT id FROM kodi_instances WHERE library_group_id = ? ORDER BY id`, id)
}

// Delete removes a group. Its members are left without a group.
func (r LibraryGroups) Delete(ctx context.Context, id int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM library_groups WHERE id = ?`, id)); err != nil {
		return fmt.Errorf("deleting library group %d: %w", id, err)
	}

	return nil
}

// DeleteEmpty removes groups that have no members and returns their IDs.
func (r LibraryGroups) DeleteEmpty(ctx context.Context) ([]int64, error) {
	ids, err := queryAll(ctx, r.db.write, scanID, `DELETE FROM library_groups WHERE id NOT IN (SELECT library_group_id FROM kodi_instances WHERE library_group_id IS NOT NULL) RETURNING id`)
	if err != nil {
		return nil, fmt.Errorf("deleting empty library groups: %w", err)
	}

	return ids, nil
}

func scanID(s scanner) (int64, error) {
	var id int64
	if err := s.Scan(&id); err != nil {
		return 0, mapErr(err)
	}

	return id, nil
}
