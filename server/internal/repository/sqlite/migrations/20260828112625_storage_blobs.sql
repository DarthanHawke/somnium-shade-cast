-- +goose Up

-- Зашифрованное хранилище файлов. Один blob может разделяться
-- несколькими треками (дедупликация по sha256 исходника)
CREATE TABLE blobs (
    id               INTEGER PRIMARY KEY,
    sha256_plain     TEXT NOT NULL UNIQUE,  -- хэш НЕзашифрованного файла (дедуп)
    size_plain       INTEGER NOT NULL,  -- размер исходника, байт
    size_encrypted   INTEGER NOT NULL,
    mime_type        TEXT NOT NULL DEFAULT 'application/octet-stream',
    kind             TEXT NOT NULL DEFAULT 'audio'
                     CHECK (kind IN ('audio','image','other')),
    enc_algo         TEXT NOT NULL DEFAULT 'aes-256-gcm',
    enc_key_wrapped  BLOB NOT NULL, -- ключ файла, обёрнут мастер-ключом
    master_key_id    TEXT NOT NULL, -- на случай ротации мастер-ключа
    chunk_size       INTEGER NOT NULL DEFAULT 131072,
    storage_path     TEXT NOT NULL,
    ref_count        INTEGER NOT NULL DEFAULT 0, -- ведёт логика приложения
    created_at       INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- +goose Down
DROP TABLE blobs;