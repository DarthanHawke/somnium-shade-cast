// internal/repository/sqlite/search.go
package sqlite

import (
	"context"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobRepository реализует методы для работы с fts для треков
type SearchRepository struct {
	db *Database
}

func NewSearchRepository(db *Database) *SearchRepository {
	return &SearchRepository{
		db: db,
	}
}

const deleteFTSTrack = "delete"

// Insert добавляет запись в FTS(без удаления существующей, т.е. только для новых)
func (r *SearchRepository) Insert(ctx context.Context, trackID domain.ID, title, artist, album string) error {
	const op = "fts.Insert"
	const query = `
		INSERT INTO tracks_fts(rowid, title, artist_name, album_title)
        VALUES ($1, $2, $3, $4)`

	exec := r.db.ExecutorFromCtx(ctx)
	if _, err := exec.ExecContext(ctx, query, trackID, title, artist, album); err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return nil
}

// Remove удаляет запись в FTS
func (r *SearchRepository) Remove(ctx context.Context, trackID domain.ID, title, artist, album string) error {
	const op = "fts.Remove"
	const query = `
		INSERT INTO tracks_fts(tracks_fts, rowid, title, artist_name, album_title)
        VALUES ($1, $2, $3, $4, $5)`

	exec := r.db.ExecutorFromCtx(ctx)
	if _, err := exec.ExecContext(ctx, query, deleteFTSTrack, trackID, title, artist, album); err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	return nil
}

// Index обновляет запись в FTS
func (r *SearchRepository) Update(ctx context.Context, trackID domain.ID, title, artist, album string) error {
	const op = "fts.Update"

	if err := r.Remove(ctx, trackID, title, artist, album); err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if err := r.Insert(ctx, trackID, title, artist, album); err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return nil
}

// Search ищет треки по запросу
func (r *SearchRepository) Search(ctx context.Context, req string, limit int) ([]domain.ID, error) {
	const op = "fts.Search"
	const query = `
		SELECT rowid FROM tracks_fts WHERE tracks_fts MATCH $1 LIMIT $2`

	exec := r.db.ExecutorFromCtx(ctx)
	var ids []domain.ID
	err := exec.SelectContext(ctx, &ids, query, req, limit)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return ids, nil
}
