-- A room's spoken name is either another name for the room ("lounge room") or a device in it ("TV"). Names that already exist are room names.

-- +goose Up
ALTER TABLE room_aliases ADD COLUMN kind TEXT NOT NULL DEFAULT 'room' CHECK (kind IN ('room', 'device'));

-- +goose Down
ALTER TABLE room_aliases DROP COLUMN kind;
