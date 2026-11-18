-- +goose Up

CREATE INDEX idx_jobs_finished ON jobs(finished_at, id)
    WHERE status IN ('done', 'dead');

-- При переименовании обновляется индекс всех загруженных треков исполнителя.
-- +goose StatementBegin
CREATE TRIGGER trg_tracks_fts_artist_rename
AFTER UPDATE OF name ON artists
WHEN NEW.name IS NOT OLD.name
BEGIN
    INSERT OR REPLACE INTO tracks_fts (rowid, title, artist_name, album_title)
    SELECT t.id, t.title, NEW.name, COALESCE(al.title, '')
    FROM tracks AS t
    LEFT JOIN albums AS al ON al.id = t.album_id
    WHERE t.artist_id = NEW.id AND t.status = 'uploaded';
END;
-- +goose StatementEnd

-- При переименовании обновляется индекс всех загруженных треков альбома.
-- +goose StatementBegin
CREATE TRIGGER trg_tracks_fts_album_rename
AFTER UPDATE OF title ON albums
WHEN NEW.title IS NOT OLD.title
BEGIN
    INSERT OR REPLACE INTO tracks_fts (rowid, title, artist_name, album_title)
    SELECT t.id, t.title, ar.name, NEW.title
    FROM tracks AS t
    JOIN artists AS ar ON ar.id = t.artist_id
    WHERE t.album_id = NEW.id AND t.status = 'uploaded';
END;
-- +goose StatementEnd

-- +goose Down

DROP TRIGGER trg_tracks_fts_album_rename;
DROP TRIGGER trg_tracks_fts_artist_rename;
DROP INDEX idx_jobs_finished;
