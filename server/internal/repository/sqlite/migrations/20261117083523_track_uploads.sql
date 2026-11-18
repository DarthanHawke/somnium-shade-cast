-- +goose Up

-- Загрузка трека хранит принятый файл и публичный результат работы очереди.
CREATE TABLE track_uploads (
    public_id         TEXT PRIMARY KEY NOT NULL,
    owner_user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key   TEXT NOT NULL,
    request_hash      TEXT NOT NULL,
    blob_id           INTEGER REFERENCES blobs(id) ON DELETE RESTRICT,
    job_id            INTEGER UNIQUE REFERENCES jobs(id) ON DELETE RESTRICT,
    original_filename TEXT NOT NULL,
    force_dup         INTEGER NOT NULL CHECK (force_dup IN (0, 1)),
    ghost_public_id   TEXT,
    fingerprint       TEXT NOT NULL,
    fp_duration       INTEGER NOT NULL CHECK (fp_duration >= 0),
    track_public_id   TEXT,
    error_code        TEXT,
    created_at        INTEGER NOT NULL,
    completed_at      INTEGER,
    UNIQUE (owner_user_id, idempotency_key),
    CHECK (
        (completed_at IS NULL AND blob_id IS NOT NULL AND job_id IS NOT NULL
            AND track_public_id IS NULL AND error_code IS NULL)
        OR
        (completed_at IS NOT NULL AND blob_id IS NULL AND job_id IS NULL
            AND (track_public_id IS NOT NULL) <> (error_code IS NOT NULL))
    )
) STRICT;

CREATE INDEX idx_track_uploads_blob ON track_uploads(blob_id)
    WHERE blob_id IS NOT NULL;
CREATE INDEX idx_track_uploads_owner_created
    ON track_uploads(owner_user_id, created_at DESC, public_id DESC);

-- Исчерпание попыток записывает публичный итог в той же транзакции.
-- +goose StatementBegin
CREATE TRIGGER trg_track_uploads_job_dead
AFTER UPDATE OF status ON jobs
WHEN NEW.type = 'upload_track' AND NEW.status = 'dead' AND OLD.status <> 'dead'
BEGIN
    UPDATE track_uploads
    SET error_code = 'processing_failed',
        completed_at = COALESCE(NEW.finished_at, CAST(round(unixepoch('subsec') * 1000) AS INTEGER)),
        job_id = NULL,
        blob_id = NULL
    WHERE job_id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down

DROP TRIGGER trg_track_uploads_job_dead;
DROP TABLE track_uploads;
DELETE FROM jobs WHERE type = 'upload_track';
