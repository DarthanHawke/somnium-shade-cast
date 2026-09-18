-- +goose Up

-- Ключ-значение для настроек сервера
CREATE TABLE server_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    public_id     TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'member'
                  CHECK (role IN ('admin','member')),
    avatar_blob_id INTEGER,
    is_active     INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0,1)),
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- Одноразовые инвайты. Храним только хэш ключа
CREATE TABLE invites (
    id           INTEGER PRIMARY KEY,
    key_hash     TEXT NOT NULL UNIQUE,  -- sha256(invite_key)
    role         TEXT NOT NULL DEFAULT 'member'
                 CHECK (role IN ('admin','member')),
    created_by   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    used_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    used_at      INTEGER,
    expires_at   INTEGER NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- Клиентские mTLS-сертификаты
CREATE TABLE client_certs (
    id             INTEGER PRIMARY KEY,
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    serial         TEXT NOT NULL UNIQUE,    -- серийник серта
    fingerprint    TEXT NOT NULL UNIQUE,    -- sha256 от DER
    device_name    TEXT NOT NULL DEFAULT '',
    not_after      INTEGER NOT NULL,    -- срок действия
    revoked_at     INTEGER, -- NULL = активен
    last_seen_at   INTEGER,
    created_at     INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_client_certs_user ON client_certs(user_id);

-- Пользовательские настройки
CREATE TABLE user_settings (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (user_id, key)
) STRICT;

-- +goose Down
DROP TABLE user_settings;
DROP TABLE client_certs;
DROP TABLE invites;
DROP TABLE users;
DROP TABLE server_settings;