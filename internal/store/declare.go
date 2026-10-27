package store

import (
	"context"
	"fmt"
)

// Declare creates the device if it doesn't exist yet, without counting a request, and returns it. Devices declared in the config file use it so they can be linked before they've spoken.
func (r EchoDevices) Declare(ctx context.Context, alexaDeviceID string) (EchoDevice, error) {
	now := toMillis(r.db.stamp())

	d, err := scanDevice(r.db.write.QueryRowContext(ctx, `INSERT INTO echo_devices (alexa_device_id, first_seen, last_seen, request_count)
		VALUES (?1, ?2, ?2, 0)
		ON CONFLICT (alexa_device_id) DO UPDATE SET alexa_device_id = excluded.alexa_device_id
		RETURNING `+deviceColumns, alexaDeviceID, now))
	if err != nil {
		return EchoDevice{}, fmt.Errorf("declaring echo device: %w", err)
	}

	return d, nil
}
