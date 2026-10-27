package store

import (
	"context"
	"database/sql"
	"fmt"
)

// EchoDevices returns the Echo device repository.
func (db *DB) EchoDevices() EchoDevices { return EchoDevices{db} }

func scanDevice(s scanner) (EchoDevice, error) {
	var (
		d           EchoDevice
		room, kodi  sql.NullInt64
		first, last int64
	)

	if err := s.Scan(&d.ID, &d.AlexaDeviceID, &d.DisplayName, &room, &kodi, &first, &last, &d.RequestCount); err != nil {
		return EchoDevice{}, mapErr(err)
	}

	d.RoomID, d.KodiInstanceID = room.Int64, kodi.Int64
	d.FirstSeen, d.LastSeen = fromMillis(first), fromMillis(last)

	return d, nil
}

// Seen records a request from an Alexa device, creating it on first sight, and returns the updated row.
func (r EchoDevices) Seen(ctx context.Context, alexaDeviceID string) (EchoDevice, error) {
	now := toMillis(r.db.stamp())

	d, err := scanDevice(r.db.write.QueryRowContext(ctx, `INSERT INTO echo_devices (alexa_device_id, first_seen, last_seen, request_count)
		VALUES (?1, ?2, ?2, 1)
		ON CONFLICT (alexa_device_id) DO UPDATE SET last_seen = excluded.last_seen, request_count = request_count + 1
		RETURNING `+deviceColumns, alexaDeviceID, now))
	if err != nil {
		return EchoDevice{}, fmt.Errorf("recording echo device: %w", err)
	}

	return d, nil
}

// Update saves the display name, room and Kodi instance of d.
func (r EchoDevices) Update(ctx context.Context, d *EchoDevice) error {
	err := affected(r.db.write.ExecContext(ctx, `UPDATE echo_devices SET display_name = ?, room_id = ?, kodi_instance_id = ? WHERE id = ?`,
		d.DisplayName, nullID(d.RoomID), nullID(d.KodiInstanceID), d.ID))
	if err != nil {
		return fmt.Errorf("updating echo device %d: %w", d.ID, err)
	}

	return nil
}

// Get returns the device with the given ID.
func (r EchoDevices) Get(ctx context.Context, id int64) (EchoDevice, error) {
	d, err := scanDevice(r.db.read.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM echo_devices WHERE id = ?`, id))
	if err != nil {
		return EchoDevice{}, fmt.Errorf("getting echo device %d: %w", id, err)
	}

	return d, nil
}

// GetByAlexaID returns the device with the given Alexa device ID.
func (r EchoDevices) GetByAlexaID(ctx context.Context, alexaDeviceID string) (EchoDevice, error) {
	d, err := scanDevice(r.db.read.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM echo_devices WHERE alexa_device_id = ?`, alexaDeviceID))
	if err != nil {
		return EchoDevice{}, fmt.Errorf("getting echo device: %w", err)
	}

	return d, nil
}

// List returns all devices, most recently seen first.
func (r EchoDevices) List(ctx context.Context) ([]EchoDevice, error) {
	return queryAll(ctx, r.db.read, scanDevice, `SELECT `+deviceColumns+` FROM echo_devices ORDER BY last_seen DESC, id`)
}

// Delete forgets a device. It is recreated if it speaks again.
func (r EchoDevices) Delete(ctx context.Context, id int64) error {
	if err := affected(r.db.write.ExecContext(ctx, `DELETE FROM echo_devices WHERE id = ?`, id)); err != nil {
		return fmt.Errorf("deleting echo device %d: %w", id, err)
	}

	return nil
}
