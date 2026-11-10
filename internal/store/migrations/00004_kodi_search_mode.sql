-- How "search for X" searches on a Kodi instance, and the skin whose search steps to use for a skin Encore doesn't know. An empty mode follows alexa.search_mode; an empty profile uses none.

-- +goose Up
ALTER TABLE kodi_instances ADD COLUMN search_mode TEXT NOT NULL DEFAULT '' CHECK (search_mode IN ('', 'library', 'global_search', 'skin'));
ALTER TABLE kodi_instances ADD COLUMN skin_profile TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE kodi_instances DROP COLUMN skin_profile;
ALTER TABLE kodi_instances DROP COLUMN search_mode;
