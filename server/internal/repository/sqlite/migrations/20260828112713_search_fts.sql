-- +goose Up

CREATE VIRTUAL TABLE tracks_fts USING fts5(
    title, artist_name, album_title,
    content='',
    contentless_delete=1,
    tokenize='unicode61 remove_diacritics 2'
);

-- +goose Down

DROP TABLE tracks_fts;