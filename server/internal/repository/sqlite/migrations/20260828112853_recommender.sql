-- +goose Up

-- Теневой профиль: EWMA-предпочтения тегов по контекстам.
-- context: 'global' | '<daypart>_<daytype>', например 'morning_weekday',
-- 'evening_weekend', 'night_weekday' (8 контекстов + global)
CREATE TABLE profile_tag_prefs (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    context    TEXT NOT NULL,
    tag        TEXT NOT NULL,
    value      REAL NOT NULL DEFAULT 0.0,      -- P[g] ∈ [-1,1]
    n_events   INTEGER NOT NULL DEFAULT 0,     -- для λ = n/(n+K)
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (user_id, context, tag)
) STRICT;

-- Предпочтения по фичам: центр вкуса μ_F и служебное состояние.
-- Вектор хранится в JSON — размерность фич будет меняться (features_version).
CREATE TABLE profile_feature_state (
    user_id           INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    features_version  INTEGER NOT NULL DEFAULT 1,
    mu_f_json         TEXT NOT NULL DEFAULT '[]',
    n_events          INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

-- Граф переходов «дослушал A -> дослушал B». Персональный.
CREATE TABLE graph_edges (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    to_track_id   INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    weight        REAL NOT NULL DEFAULT 0.0,
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch()*1000),
    PRIMARY KEY (user_id, from_track_id, to_track_id),
    CHECK (from_track_id != to_track_id)
) STRICT;

CREATE INDEX idx_graph_from ON graph_edges(user_id, from_track_id);

-- +goose Down
DROP TABLE graph_edges;
DROP TABLE profile_feature_state;
DROP TABLE profile_tag_prefs;