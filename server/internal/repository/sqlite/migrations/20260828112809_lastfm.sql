-- +goose Up

-- Привязка Last.fm на пользователя. Всё чувствительное — зашифровано
-- мастер-ключом (в колонках *_enc лежит nonce+ciphertext)
CREATE TABLE lastfm_accounts (
    user_id          INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    lastfm_username  TEXT NOT NULL,
    api_key_enc      BLOB NOT NULL, -- пользовательский API key
    api_secret_enc   BLOB NOT NULL,
    session_key_enc  BLOB NOT NULL,
    -- веб сессия для персональных релизов
    web_cookies_enc  BLOB,
    scrobbling_on    INTEGER NOT NULL DEFAULT 1 CHECK (scrobbling_on IN (0,1)),
    connected_at     INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- Очередь скробблов с ретраями
CREATE TABLE scrobble_queue (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at   INTEGER NOT NULL,   -- unix ms начала прослушивания
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','sent','failed','dead')),
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_scrobble_pick ON scrobble_queue(status, user_id)
    WHERE status IN ('pending','failed');
-- защита от двойного скробла
CREATE UNIQUE INDEX idx_scrobble_dedup ON scrobble_queue(user_id, track_id, played_at);

-- Полпулярность: еженедельные снимки playcount. Храним историю снимков,
-- дельта за неделю - разница двух последних
CREATE TABLE track_trends (
    track_id     INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    fetched_at   INTEGER NOT NULL,
    playcount    INTEGER NOT NULL,  -- глобальный playcount на Last.fm
    listeners    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, fetched_at)
) STRICT;

-- Материализованная дельта (обновляется job после снятия снимка),
-- чтобы рекомендатель не считал разницу на лету
CREATE TABLE track_trend_current (
    track_id        INTEGER PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    weekly_delta    INTEGER NOT NULL DEFAULT 0,
    trend_norm      REAL NOT NULL DEFAULT 0.0,   -- лог-нормировка [0,1] по библиотеке
    updated_at      INTEGER NOT NULL
) STRICT;

-- Кэш ленты релизов (скрейпер Last.fm / MusicBrainz-фолбэк)
CREATE TABLE news_releases (
    id           INTEGER PRIMARY KEY,
    provider     TEXT NOT NULL CHECK (provider IN ('lastfm','musicbrainz')),
    kind         TEXT NOT NULL CHECK (kind IN ('out_now','coming_soon')),
    artist_name  TEXT NOT NULL,
    album_title  TEXT NOT NULL,
    release_date INTEGER,
    image_url    TEXT NOT NULL DEFAULT '',
    external_url TEXT NOT NULL DEFAULT '', 
    for_user_id  INTEGER REFERENCES users(id) ON DELETE CASCADE,
    fetched_at   INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_news_feed ON news_releases(kind, for_user_id, fetched_at);
CREATE UNIQUE INDEX idx_news_dedup
    ON news_releases(provider, kind, artist_name, album_title,
                     COALESCE(for_user_id, 0));

-- +goose Down
DROP TABLE news_releases;
DROP TABLE track_trend_current;
DROP TABLE track_trends;
DROP TABLE scrobble_queue;
DROP TABLE lastfm_accounts;