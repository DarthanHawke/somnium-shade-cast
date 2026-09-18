// Пакет sqlite реализовывает работу с sqlite
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// AlbumRepository реализует методы для работы с библиотекой альбомов
type AlbumRepository struct {
	db *Database
}

func NewAlbumRepository(db *Database) *AlbumRepository {
	return &AlbumRepository{
		db: db,
	}
}

// Create - создает новый альбом
func (r *AlbumRepository) Create(ctx context.Context, album *domain.Album) error {
	const op = "album.Create"

	const query = `
		INSERT INTO albums (public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at)
		VALUES (:public_id, :artist_id, :title, :norm_title, :year, :release_date, 
					:mbid, :lastfm_url, :cover_blob_id, :total_tracks, :is_complete,
					:enrich_status, :enriched_at, :created_at, :updated_at)`

	res, err := r.db.NamedExecContext(ctx, query, album)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	id, _ := res.LastInsertId()
	album.ID = domain.NewID(id)
	return nil
}

// GetByPublicID - возвращает альбом по ULID
func (r *AlbumRepository) GetByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
) (*domain.Album, error) {
	const op = "album.GetByPublicID"

	const query = `
		SELECT id, public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at
		FROM albums WHERE public_id = $1`

	var album domain.Album
	err := r.db.GetContext(ctx, &album, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &album, nil
}

// Update - обновляет данные исполнителя
func (r *AlbumRepository) Update(ctx context.Context, album *domain.Album) error {
	const op = "album.Update"

	const query = `
		UPDATE albums 
		SET artist_id = :artist_id, title = :title, norm_title = :norm_title, year = :year, 
			release_date = :release_date, mbid = :mbid, lastfm_url = :lastfm_url, cover_blob_id = :cover_blob_id, 
			total_tracks = :total_tracks, is_complete = :is_complete, enrich_status = :enrich_status, 
			enriched_at = :enriched_at, updated_at = :updated_at
		WHERE public_id = :public_id`

	result, err := r.db.NamedExecContext(ctx, query, album)
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Delete - хард удаление исполнителя
func (r *AlbumRepository) Delete(ctx context.Context, tx *sql.Tx, publicID domain.PublicID) error {
	const op = "album.Delete"

	const query = `DELETE FROM albums WHERE public_id = $1`

	res, err := tx.ExecContext(ctx, query, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// List - возвращает испольнителей
func (r *AlbumRepository) List(
	ctx context.Context,
	offset, limit int,
) ([]domain.Album, error) {
	const op = "album.List"

	const query = `
		SELECT id, public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at
		FROM albums 
		ORDER BY norm_title
		LIMIT $1 OFFSET $2`

	var albums []domain.Album
	err := r.db.SelectContext(ctx, &albums, query, limit, offset)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return albums, nil
}

// Search - возвращает испольнителей по имени
func (r *AlbumRepository) Search(
	ctx context.Context,
	pattern string,
	offset, limit int,
) ([]domain.Album, error) {
	const op = "album.Search"

	const query = `
		SELECT id, public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at
		FROM albums
		WHERE name LIKE $1 OR sort_name LIKE $1 
		ORDER BY norm_title
		LIMIT $2 OFFSET $3`

	var albums []domain.Album
	err := r.db.SelectContext(ctx, &albums, query, pattern, limit, offset)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return albums, nil
}
