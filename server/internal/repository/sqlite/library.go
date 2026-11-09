package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

// LibraryRepository получает представления треков с данными исполнителей и альбомов.
type LibraryRepository struct {
	db *Database
}

// NewLibraryRepository создаёт репозиторий, использующий db.
func NewLibraryRepository(db *Database) *LibraryRepository {
	return &LibraryRepository{
		db: db,
	}
}

const selectTrackView = `
	SELECT
		t.public_id,
		t.title,
		t.duration_ms,
		t.status,
		t.track_no,
		t.disc_no,
		t.codec,
		t.bitrate_kbps,
		t.sample_rate,
		COALESCE(t.lastfm_url, '') AS lastfm_url,
		t.created_at,
		t.updated_at,
		ar.public_id AS artist_public_id,
		ar.name AS artist_name,

		al.public_id AS album_public_id,
		al.title AS album_title,
		al.cover_blob_id

	FROM tracks AS t
	JOIN artists AS ar ON ar.id = t.artist_id
	LEFT JOIN albums AS al ON al.id = t.album_id
`

// GetReadableTrackByPublicID возвращает представление трека, доступного
// для чтения пользователю viewerID. Альбом может отсутствовать.
// Если трек отсутствует или недоступен, возвращает domain.ErrNotFound.
func (r *LibraryRepository) GetReadableTrackByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
	viewerID domain.ID,
) (*library.TrackView, error) {
	const op = "library.GetReadableTrackByPublicID"
	const query = selectTrackView + readableTrackByPublicIDWhere

	exec := r.db.executor(ctx)
	var view library.TrackView
	err := exec.GetContext(ctx, &view, query, publicID, viewerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &view, nil
}

// ListTracksByOwner возвращает представления треков владельца ownerID.
// Результат упорядочен по CreatedAt и внутреннему ID трека по убыванию.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
func (r *LibraryRepository) ListTracksByOwner(
	ctx context.Context,
	ownerID domain.ID,
	offset, limit int,
) ([]library.TrackView, error) {
	const op = "library.ListTracksByOwner"

	const query = selectTrackView + `
		WHERE t.owner_user_id = $1
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT $2 OFFSET $3
	`
	views := make([]library.TrackView, 0)

	exec := r.db.executor(ctx)
	if err := exec.SelectContext(
		ctx,
		&views,
		query,
		ownerID,
		limit,
		offset,
	); err != nil {
		return nil, wrapDBError(op, err)
	}

	return views, nil
}
