-- Профили подключений к серверам
CREATE TABLE server_profiles (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL,
    host          TEXT NOT NULL,
    api_port      INTEGER NOT NULL DEFAULT 8443,
    ca_cert_pem   TEXT NOT NULL,
    client_cert_ref TEXT NOT NULL,
    my_user_public_id TEXT NOT NULL,
    is_default    INTEGER NOT NULL DEFAULT 0,
    ssh_ref       TEXT,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000)
);

-- Манифест локальных файлов (Source Resolver)
CREATE TABLE local_tracks (
    server_track_public_id TEXT NOT NULL,
    server_profile_id INTEGER NOT NULL REFERENCES server_profiles(id) ON DELETE CASCADE,
    local_path    TEXT NOT NULL,
    sha256        TEXT NOT NULL,
    size          INTEGER NOT NULL,
    source        TEXT NOT NULL CHECK (source IN ('downloaded','linked','uploaded')),
    metadata_rev  INTEGER NOT NULL DEFAULT 0, -- для сверки с /library/manifest
    verified_at   INTEGER,
    PRIMARY KEY (server_profile_id, server_track_public_id)
);

-- Офлайн очередь событий (скипы/прослушивания шлём при появлении сети)
CREATE TABLE pending_events (
    id          INTEGER PRIMARY KEY,
    server_profile_id INTEGER NOT NULL REFERENCES server_profiles(id) ON DELETE CASCADE,
    endpoint    TEXT NOT NULL,  -- '/flow/feedback' | '/scrobbles'
    payload     TEXT NOT NULL,  -- JSON
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000)
);

-- Кэш библиотеки для быстрого UI и офлайн-просмотра
CREATE TABLE cached_library (
    server_profile_id INTEGER NOT NULL,
    entity      TEXT NOT NULL CHECK (entity IN ('track','album','artist')),
    public_id   TEXT NOT NULL,
    data_json   TEXT NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (server_profile_id, entity, public_id)
);