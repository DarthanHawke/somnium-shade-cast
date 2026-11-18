-- +goose Up

-- Очередь фоновых задач: анализ аудио, обогащение,
-- скроблинг-ретраи, снятие трендов, скрейп релизов, пересчёт профиля
CREATE TABLE jobs (
    id           INTEGER PRIMARY KEY,
    type         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'running', 'done', 'failed', 'dead')),
    priority     INTEGER NOT NULL DEFAULT 5,
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    run_after    INTEGER NOT NULL DEFAULT 0,
    run_every_ms INTEGER NOT NULL DEFAULT 0 CHECK (run_every_ms >= 0),
    dedup_key    TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    lease_token  TEXT,
    lease_until  INTEGER,
    started_at   INTEGER,
    finished_at  INTEGER,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch() * 1000)
) STRICT;

CREATE INDEX idx_jobs_ready ON jobs(type, run_after, priority DESC, id)
    WHERE status IN ('pending', 'failed');

CREATE INDEX idx_jobs_expired ON jobs(type, lease_until)
    WHERE status = 'running';

CREATE UNIQUE INDEX idx_jobs_active_key ON jobs(type, dedup_key)
    WHERE dedup_key IS NOT NULL AND status IN ('pending', 'failed', 'running');

CREATE INDEX idx_blobs_storage_path ON blobs(storage_path);
CREATE INDEX idx_blobs_orphans ON blobs(created_at, id)
    WHERE ref_count = 0;
CREATE INDEX idx_artists_image_blob ON artists(image_blob_id)
    WHERE image_blob_id IS NOT NULL;
CREATE INDEX idx_albums_cover_blob ON albums(cover_blob_id)
    WHERE cover_blob_id IS NOT NULL;


-- +goose Down
DROP INDEX idx_albums_cover_blob;
DROP INDEX idx_artists_image_blob;
DROP INDEX idx_blobs_orphans;
DROP INDEX idx_blobs_storage_path;
DROP TABLE jobs;