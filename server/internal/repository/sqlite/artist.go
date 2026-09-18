// Пакет sqlite реализовывает работу с sqlite
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// ArtistRepository реализует методы для работы с библиотекой исполнителей
type ArtistRepository struct {
	db *Database
}

func NewArtistRepository(db *Database) *ArtistRepository {
	return &ArtistRepository{
		db: db,
	}
}

// Create - создает нового исполнителя
func (r *ArtistRepository) Create(ctx context.Context, artist *domain.Artist) error {
	const op = "artist.Create"

	const query = `
		INSERT INTO artists (public_id, name, norm_name, sort_name, mbid, lastfm_url, 
					image_blob_id, bio, enrich_status, enriched_at, created_at, updated_at)
		VALUES (:public_id, :name, :norm_name, :sort_name, :mbid, :lastfm_url, 
					:image_blob_id, :bio, :enrich_status, :enriched_at, :created_at, :updated_at)`

	res, err := r.db.NamedExecContext(ctx, query, artist)
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
	artist.ID = domain.NewID(id)
	return nil
}

// GetByPublicID - возвращает исполнителя по ULID
func (r *ArtistRepository) GetByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
) (*domain.Artist, error) {
	const op = "artist.GetByPublicID"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists WHERE public_id = $1`

	var artist domain.Artist
	err := r.db.GetContext(ctx, &artist, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &artist, nil
}

// Update - обновляет данные исполнителя
func (r *ArtistRepository) Update(ctx context.Context, artist *domain.Artist) error {
	const op = "artist.Update"

	const query = `
		UPDATE artists 
		SET name = :name, norm_name = :norm_name, sort_name = :sort_name, mbid = :mbid, 
			lastfm_url = :lastfm_url, image_blob_id = :image_blob_id, bio = :bio, 
			enrich_status = :enrich_status, enriched_at = :enriched_at, updated_at = :updated_at
		WHERE public_id = :public_id`

	result, err := r.db.NamedExecContext(ctx, query, artist)
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
func (r *ArtistRepository) Delete(ctx context.Context, tx *sql.Tx, publicID domain.PublicID) error {
	const op = "artist.Delete"

	const query = `DELETE FROM artists WHERE public_id = $1`

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
func (r *ArtistRepository) List(
	ctx context.Context,
	offset, limit int,
) ([]domain.Artist, error) {
	const op = "artist.List"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists 
		ORDER BY sort_name
		LIMIT $1 OFFSET $2`

	var artists []domain.Artist
	err := r.db.SelectContext(ctx, &artists, query, limit, offset)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return artists, nil
}

// Search - возвращает испольнителей по имени
func (r *ArtistRepository) Search(
	ctx context.Context,
	pattern string,
	offset, limit int,
) ([]domain.Artist, error) {
	const op = "artist.Search"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists
		WHERE name LIKE $1 OR sort_name LIKE $1 
		ORDER BY sort_name
		LIMIT $2 OFFSET $3`

	var artists []domain.Artist
	err := r.db.SelectContext(ctx, &artists, pattern, query, limit, offset)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return artists, nil
}
