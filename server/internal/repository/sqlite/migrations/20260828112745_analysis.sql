-- +goose Up

-- Ааудио анализ. features_version — чтоб при улучшении
-- пайплайна переанализировать только устаревшие треки
CREATE TABLE track_features (
    track_id          INTEGER PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    features_version  INTEGER NOT NULL DEFAULT 1,
    bpm               REAL,
    energy            REAL,        -- RMS-производная, [0,1] после нормализации
    loudness_lufs     REAL,
    spectral_centroid REAL,        -- «яркость»
    dynamic_range     REAL,        -- crest factor
    instrumentalness  REAL,        -- грубая оценка [0,1]
    -- позиции припевов/повторяющихся сегментов: JSON [{start_ms, end_ms, conf}]
    chorus_json       TEXT NOT NULL DEFAULT '[]',
    -- нормализованный вектор фич для рекомендателя (пересчитывается при
    -- изменении статистики библиотеки): JSON-массив чисел
    feat_vector_json  TEXT NOT NULL DEFAULT '[]',
    analyzed_at       INTEGER NOT NULL DEFAULT (unixepoch()*1000)
) STRICT;

CREATE INDEX idx_features_version ON track_features(features_version);

-- +goose Down
DROP TABLE track_features;