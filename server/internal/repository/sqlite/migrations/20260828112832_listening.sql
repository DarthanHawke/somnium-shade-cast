-- +goose Up

-- Сессии потока. pattern_state_json - снапшот SessionState для
-- восстановления после рестарта сервера/переподключения клиента
CREATE TABLE sessions (
    id                  INTEGER PRIMARY KEY,
    public_id           TEXT NOT NULL UNIQUE,
    user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    started_at          INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    last_activity_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    ended_at            INTEGER,
    pattern_state_json  TEXT NOT NULL DEFAULT '{}'
) STRICT;

CREATE INDEX idx_sessions_user ON sessions(user_id, started_at);

-- Главный лог для рекомендаций и статистики. Append-only
CREATE TABLE listen_events (
    id                INTEGER PRIMARY KEY,
    user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id          INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    session_id        INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    started_at        INTEGER NOT NULL, -- unix ms
    listened_ms       INTEGER NOT NULL,
    track_duration_ms INTEGER NOT NULL,
    skip_position_ms  INTEGER,  -- NULL если дослушан
    -- контекст запуска: поток / альбом подряд / ручной выбор / чужая библиотека
    context           TEXT NOT NULL DEFAULT 'manual'
                      CHECK (context IN ('flow','album','manual','other_user')),
    source            TEXT NOT NULL DEFAULT 'stream'
                      CHECK (source IN ('stream','local')),  -- источник (с серевра или с клиента)
    dow               INTEGER NOT NULL CHECK (dow BETWEEN 0 AND 6),
    hour              INTEGER NOT NULL CHECK (hour BETWEEN 0 AND 23),
    reward            REAL NOT NULL DEFAULT 0.0,
    chorus_skip       INTEGER NOT NULL DEFAULT 0 CHECK (chorus_skip IN (0,1)),
    created_at        INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_listen_user_time  ON listen_events(user_id, started_at);
CREATE INDEX idx_listen_track      ON listen_events(track_id);
CREATE INDEX idx_listen_session    ON listen_events(session_id);

-- Реакции: лайк/дизлайк/бан (бан - никогда не предлагать в потоке)
CREATE TABLE track_reactions (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    reaction    TEXT NOT NULL CHECK (reaction IN ('like','dislike','ban')),
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (user_id, track_id)
) STRICT;

-- +goose Down
DROP TABLE track_reactions;
DROP TABLE listen_events;
DROP TABLE sessions;