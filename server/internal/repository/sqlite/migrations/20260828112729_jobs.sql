-- +goose Up

-- Очередь фоновых задач: анализ аудио, обогащение,
-- скроблинг-ретраи, снятие трендов, скрейп релизов, пересчёт профиля
CREATE TABLE jobs (
    id           INTEGER PRIMARY KEY,
    type         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',    -- JSON
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','running','done','failed','dead')),
    priority     INTEGER NOT NULL DEFAULT 5,
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    run_after    INTEGER NOT NULL DEFAULT 0,
    dedup_key    TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    started_at   INTEGER,
    finished_at  INTEGER,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_jobs_pick ON jobs(status, run_after, priority)
    WHERE status IN ('pending','failed');
CREATE UNIQUE INDEX idx_jobs_dedup ON jobs(dedup_key)
    WHERE dedup_key IS NOT NULL AND status IN ('pending','running');

-- +goose Down
DROP TABLE jobs;