package sqlite

import (
	"context"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// SearchRepository поддерживает полнотекстовый индекс треков FTS5.
type SearchRepository struct {
	db *Database
}

// NewSearchRepository создаёт репозиторий, использующий db.
func NewSearchRepository(db *Database) *SearchRepository {
	return &SearchRepository{
		db: db,
	}
}

const deleteFTSTrack = "delete"

// Insert добавляет в полнотекстовый индекс название трека,
// имя исполнителя и название альбома для trackID.
func (r *SearchRepository) Insert(
	ctx context.Context,
	trackID domain.ID,
	title, artistName, albumTitle string,
) error {
	const op = "fts.Insert"
	const query = `
		INSERT INTO tracks_fts(rowid, title, artist_name, album_title)
        VALUES ($1, $2, $3, $4)`

	exec := r.db.executor(ctx)
	if _, err := exec.ExecContext(ctx, query, trackID, title, artistName, albumTitle); err != nil {
		return wrapDBError(op, err)
	}
	return nil
}

// Remove удаляет запись трека из полнотекстового индекса.
// title, artistName и albumTitle должны соответствовать значениям,
// использованным при индексации.
func (r *SearchRepository) Remove(
	ctx context.Context,
	trackID domain.ID,
	title, artistName, albumTitle string,
) error {
	const op = "fts.Remove"
	// В contentless-таблице FTS5 запись удаляется через INSERT.
	const query = `
		INSERT INTO tracks_fts(tracks_fts, rowid, title, artist_name, album_title)
        VALUES ($1, $2, $3, $4, $5)`

	exec := r.db.executor(ctx)
	if _, err := exec.ExecContext(ctx, query, deleteFTSTrack, trackID, title, artistName, albumTitle); err != nil {
		return wrapDBError(op, err)
	}

	return nil
}

// Search возвращает внутренние идентификаторы треков, соответствующих выражению FTS5 expression.
// Возвращает не более limit результатов. Порядок результатов не определён.
func (r *SearchRepository) Search(ctx context.Context, expression string, limit int) ([]domain.ID, error) {
	const op = "fts.Search"
	const query = `
		SELECT rowid FROM tracks_fts WHERE tracks_fts MATCH $1 LIMIT $2`

	exec := r.db.executor(ctx)
	var ids []domain.ID
	err := exec.SelectContext(ctx, &ids, query, expression, limit)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return ids, nil
}
