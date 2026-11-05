-- Which copy of a film or episode a Kodi instance plays when the library holds it more than once, and the screen it plays on. An empty preference follows alexa.preferred_quality; screen is the largest picture Kodi reported, 0 by 0 until it has.

-- +goose Up
ALTER TABLE kodi_instances ADD COLUMN preferred_quality TEXT NOT NULL DEFAULT '' CHECK (preferred_quality IN ('', 'screen', 'highest', '2160p', '1080p', '720p', 'sd'));
ALTER TABLE kodi_instances ADD COLUMN screen_width INTEGER NOT NULL DEFAULT 0 CHECK (screen_width >= 0);
ALTER TABLE kodi_instances ADD COLUMN screen_height INTEGER NOT NULL DEFAULT 0 CHECK (screen_height >= 0);

-- +goose Down
ALTER TABLE kodi_instances DROP COLUMN screen_height;
ALTER TABLE kodi_instances DROP COLUMN screen_width;
ALTER TABLE kodi_instances DROP COLUMN preferred_quality;
