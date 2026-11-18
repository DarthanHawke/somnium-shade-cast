package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/library"
)

// LibraryRepository читает представления треков и удаляет треки из библиотеки.
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
		t.channels,
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

// SearchTracksByOwner возвращает страницу загруженных треков владельца.
// Ищет по началу каждого слова в названии трека, имени исполнителя или названии альбома.
// Сортирует по релевантности, а при равных оценках — по ID трека по убыванию.
func (r *LibraryRepository) SearchTracksByOwner(
	ctx context.Context,
	ownerID domain.ID,
	queryText string,
	offset, limit int,
) ([]library.TrackView, error) {
	const op = "library.SearchTracksByOwner"
	const query = selectTrackView + `
		JOIN tracks_fts ON tracks_fts.rowid = t.id
		WHERE t.owner_user_id = $1
		  AND t.status = 'uploaded'
		  AND tracks_fts MATCH $2
		ORDER BY tracks_fts.rank, t.id DESC
		LIMIT $3 OFFSET $4
	`

	views := make([]library.TrackView, 0)
	expression := trackSearchExpression(queryText)
	if expression == "" {
		return views, nil
	}
	if err := r.db.executor(ctx).SelectContext(ctx, &views, query, ownerID, expression, limit, offset); err != nil {
		return nil, wrapDBError(op, err)
	}
	return views, nil
}

// trackSearchExpression преобразует строку поиска в выражение FTS5.
// Кавычки заставляют FTS5 трактовать слова вроде OR как обычный текст.
func trackSearchExpression(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) &&
			!unicode.IsMark(r) && !unicode.Is(unicode.Co, r)
	})
	for i, word := range words {
		words[i] = `"` + word + `"*`
	}
	return strings.Join(words, " AND ")
}

// RemoveTrack удаляет один трек владельца по публичному ID и возвращает число удалённых треков.
func (r *LibraryRepository) RemoveTrack(ctx context.Context, ownerID domain.ID, publicID domain.PublicID) (int, error) {
	const filter = "AND public_id = $2"
	return r.removeTracks(ctx, ownerID, filter, publicID)
}

// RemoveAlbumTracks удаляет треки владельца из указанного альбома и возвращает их число.
func (r *LibraryRepository) RemoveAlbumTracks(ctx context.Context, ownerID domain.ID, albumID domain.PublicID) (int, error) {
	const filter = "AND album_id = (SELECT id FROM albums WHERE public_id = $2)"
	return r.removeTracks(ctx, ownerID, filter, albumID)
}

// RemoveArtistTracks удаляет треки владельца с указанным исполнителем и возвращает их число.
func (r *LibraryRepository) RemoveArtistTracks(ctx context.Context, ownerID domain.ID, artistID domain.PublicID) (int, error) {
	const filter = "AND artist_id = (SELECT id FROM artists WHERE public_id = $2)"
	return r.removeTracks(ctx, ownerID, filter, artistID)
}

// removeTracks удаляет треки, их записи FTS и ссылки на блобы.
// Использует транзакцию из ctx или открывает собственную.
// filter содержит SQL из вызывающего метода. Значения передаются через filterArgs.
func (r *LibraryRepository) removeTracks(
	ctx context.Context,
	ownerID domain.ID,
	filter string,
	filterArgs ...any,
) (int, error) {
	const op = "library.removeTracks"
	const baseQuery = `DELETE FROM tracks WHERE owner_user_id = $1 `
	const returning = ` RETURNING id, blob_id`
	args := append([]any{ownerID}, filterArgs...)

	var count int
	remove := func(ctx context.Context) error {
		var removed []struct {
			ID     domain.ID  `db:"id"`
			BlobID *domain.ID `db:"blob_id"`
		}
		if err := r.db.executor(ctx).SelectContext(
			ctx,
			&removed,
			baseQuery+filter+returning,
			args...,
		); err != nil {
			return wrapDBError(op, err)
		}

		search := NewSearchRepository(r.db)
		blobs := NewBlobRepository(r.db)
		for _, track := range removed {
			if err := search.Remove(ctx, track.ID); err != nil {
				return err
			}
			if track.BlobID != nil {
				if _, err := blobs.DecRef(ctx, *track.BlobID); err != nil {
					return err
				}
			}
		}
		count = len(removed)
		return nil
	}

	var err error
	if txFromContext(ctx) != nil {
		err = remove(ctx)
	} else {
		err = NewTxRepository(r.db).WithTransaction(ctx, remove)
	}
	if err != nil {
		return 0, err
	}
	return count, nil
}
