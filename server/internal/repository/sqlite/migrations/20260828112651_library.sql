-- +goose Up

CREATE TABLE artists (
    id           INTEGER PRIMARY KEY,
    public_id    TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    sort_name    TEXT NOT NULL DEFAULT '',
    mbid         TEXT,
    lastfm_url   TEXT,
    image_blob_id INTEGER REFERENCES blobs(id) ON DELETE SET NULL,
    bio          TEXT NOT NULL DEFAULT '',
    -- обогащение:
    enrich_status TEXT NOT NULL DEFAULT 'pending'
                  CHECK (enrich_status IN ('pending','done','failed','manual')),
    enriched_at  INTEGER,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    updated_at   INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- Уникальность по нормализованному имени
ALTER TABLE artists ADD COLUMN norm_name TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_artists_norm ON artists(norm_name);

CREATE TABLE albums (
    id            INTEGER PRIMARY KEY,
    public_id     TEXT NOT NULL UNIQUE,
    artist_id     INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    title         TEXT NOT NULL,
    norm_title    TEXT NOT NULL DEFAULT '',
    year          INTEGER,
    release_date  INTEGER,
    mbid          TEXT,
    lastfm_url    TEXT,
    cover_blob_id INTEGER REFERENCES blobs(id) ON DELETE SET NULL,
    total_tracks  INTEGER,
    is_complete   INTEGER NOT NULL DEFAULT 0 CHECK (is_complete IN (0,1)),
    enrich_status TEXT NOT NULL DEFAULT 'pending'
                  CHECK (enrich_status IN ('pending','done','failed','manual')),
    enriched_at   INTEGER,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE UNIQUE INDEX idx_albums_artist_title ON albums(artist_id, norm_title);

CREATE TABLE tracks (
    id             INTEGER PRIMARY KEY,
    public_id      TEXT NOT NULL UNIQUE,
    owner_user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    album_id       INTEGER REFERENCES albums(id) ON DELETE SET NULL,
    artist_id      INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    title          TEXT NOT NULL,
    norm_title     TEXT NOT NULL DEFAULT '',
    track_no       INTEGER,
    disc_no        INTEGER NOT NULL DEFAULT 1,
    duration_ms    INTEGER,
    -- статус: uploaded если есть файл; ghost если запись из треклиста альбома без файла
    status         TEXT NOT NULL DEFAULT 'uploaded'
                   CHECK (status IN ('uploaded','ghost')),
    blob_id        INTEGER REFERENCES blobs(id) ON DELETE SET NULL,
    -- дедупликация:
    fingerprint    TEXT,    -- chromaprint
    fp_duration    INTEGER, -- длительность, с которой считался fp
    is_intentional_dup INTEGER NOT NULL DEFAULT 0 CHECK (is_intentional_dup IN (0,1)),
    -- источник и техинфо:
    original_filename TEXT NOT NULL DEFAULT '',
    codec          TEXT NOT NULL DEFAULT '',
    bitrate_kbps   INTEGER,
    sample_rate    INTEGER,
    mbid           TEXT,
    lastfm_url     TEXT,
    enrich_status  TEXT NOT NULL DEFAULT 'pending'
                   CHECK (enrich_status IN ('pending','done','failed','manual')),
    enriched_at    INTEGER,
    -- если юзер правил метаданные руками - обогащением:
    manual_override INTEGER NOT NULL DEFAULT 0 CHECK (manual_override IN (0,1)),
    created_at     INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    updated_at     INTEGER NOT NULL DEFAULT (unixepoch()*1000),
        -- инварианты статуса:
    CHECK (status != 'uploaded' OR blob_id IS NOT NULL)
) STRICT;

CREATE INDEX idx_tracks_owner      ON tracks(owner_user_id, status);
CREATE INDEX idx_tracks_album      ON tracks(album_id, disc_no, track_no);
CREATE INDEX idx_tracks_artist     ON tracks(artist_id);
CREATE INDEX idx_tracks_fp         ON tracks(fingerprint) WHERE fingerprint IS NOT NULL;
CREATE INDEX idx_tracks_blob       ON tracks(blob_id) WHERE blob_id IS NOT NULL;

-- Теги с источниками и приоритетами. Один тег может прийти из
-- нескольких источников - выбираем по приоритету
-- (user > file > lastfm > auto)
CREATE TABLE track_tags (
    track_id   INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    tag        TEXT NOT NULL,   -- нормализован: lower, trim
    source     TEXT NOT NULL
               CHECK (source IN ('file','lastfm','auto','user')),
    value      REAL NOT NULL DEFAULT 1.0 CHECK (value >= 0.0 AND value <= 1.0),
    kind       TEXT NOT NULL DEFAULT 'genre'
               CHECK (kind IN ('genre','mood','internal','custom')),
    created_at INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (track_id, tag, source)
) STRICT;

CREATE INDEX idx_track_tags_tag ON track_tags(tag, kind);

-- Теги уровня исполнителя/альбома
CREATE TABLE artist_tags (
    artist_id  INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    tag        TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('lastfm','user')),
    value      REAL NOT NULL DEFAULT 1.0,
    PRIMARY KEY (artist_id, tag, source)
) STRICT;

-- +goose Down
DROP TABLE artist_tags;
DROP TABLE track_tags;
DROP TABLE tracks;
DROP TABLE albums;
DROP TABLE artists;