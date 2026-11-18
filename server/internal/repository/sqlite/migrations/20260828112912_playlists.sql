-- +goose Up

CREATE TABLE playlists (
    id          INTEGER PRIMARY KEY,
    public_id   TEXT NOT NULL UNIQUE,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'manual'
                CHECK (kind IN ('manual','system_favorites')),
    is_public   INTEGER NOT NULL DEFAULT 1 CHECK (is_public IN (0,1)),
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    updated_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE TABLE playlist_tracks (
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    added_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (playlist_id, track_id)
) STRICT;

CREATE INDEX idx_playlist_pos ON playlist_tracks(playlist_id, position);

-- +goose Down
DROP TABLE playlist_tracks;
DROP TABLE playlists;