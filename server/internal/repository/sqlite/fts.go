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

// IndexTrack добавляет или обновляет запись загруженного трека в FTS.
// Текст берётся из актуальных записей трека, исполнителя и альбома.
// Если загруженный трек с таким ID отсутствует, возвращает domain.ErrNotFound.
func (r *SearchRepository) IndexTrack(ctx context.Context, trackID domain.ID) error {
	const op = "fts.IndexTrack"
	const query = `
		INSERT OR REPLACE INTO tracks_fts (rowid, title, artist_name, album_title)
		SELECT t.id, t.title, ar.name, COALESCE(al.title, '')
		FROM tracks AS t
		JOIN artists AS ar ON ar.id = t.artist_id
		LEFT JOIN albums AS al ON al.id = t.album_id
		WHERE t.id = $1 AND t.status = 'uploaded'`

	result, err := r.db.executor(ctx).ExecContext(ctx, query, trackID)
	if err != nil {
		return wrapDBError(op, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return wrapDBError(op, err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Remove удаляет запись трека из FTS. Отсутствие записи не считается ошибкой.
func (r *SearchRepository) Remove(ctx context.Context, trackID domain.ID) error {
	const op = "fts.Remove"
	const query = `DELETE FROM tracks_fts WHERE rowid = $1`

	if _, err := r.db.executor(ctx).ExecContext(ctx, query, trackID); err != nil {
		return wrapDBError(op, err)
	}
	return nil
}
