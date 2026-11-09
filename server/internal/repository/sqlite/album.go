package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// AlbumRepository сохраняет и получает данные альбомов в SQLite.
type AlbumRepository struct {
	db *Database
}

// NewAlbumRepository создаёт репозиторий, использующий db.
func NewAlbumRepository(db *Database) *AlbumRepository {
	return &AlbumRepository{
		db: db,
	}
}

// Insert сохраняет альбом и при успехе заполняет album.ID.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *AlbumRepository) Insert(ctx context.Context, album *domain.Album) error {
	const op = "album.Insert"

	const query = `
		INSERT INTO albums (public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at)
		VALUES (:public_id, :artist_id, :title, :norm_title, :year, :release_date, 
					:mbid, :lastfm_url, :cover_blob_id, :total_tracks, :is_complete,
					:enrich_status, :enriched_at, :created_at, :updated_at)`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, album)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return wrapDBError(op, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return wrapDBError(op+": get inserted id", err)
	}
	album.ID = domain.ID(id)
	return nil
}

// GetByArtistAndNormTitle возвращает альбом исполнителя artistID
// по нормализованному названию normTitle.
// Если альбом отсутствует, возвращает domain.ErrNotFound.
func (r *AlbumRepository) GetByArtistAndNormTitle(
	ctx context.Context,
	artistID domain.ID,
	normTitle string,
) (*domain.Album, error) {
	const op = "album.GetByArtistAndNormTitle"
	const query = `
		SELECT id, public_id, artist_id, title, norm_title, year, release_date,
			mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
			enrich_status, enriched_at, created_at, updated_at
		FROM albums WHERE artist_id = $1 AND norm_title = $2`

	exec := r.db.executor(ctx)
	var album domain.Album
	err := exec.GetContext(ctx, &album, query, artistID, normTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &album, nil
}

// GetByID возвращает альбом по внутреннему идентификатору domain.ID.
// Если альбом отсутствует, возвращает domain.ErrNotFound.
func (r *AlbumRepository) GetByID(
	ctx context.Context,
	id domain.ID,
) (*domain.Album, error) {
	const op = "album.GetByID"

	const query = `
		SELECT id, public_id, artist_id, title, norm_title, year, release_date, 
					mbid, lastfm_url, cover_blob_id, total_tracks, is_complete,
					enrich_status, enriched_at, created_at, updated_at
		FROM albums WHERE id = $1`

	exec := r.db.executor(ctx)
	var album domain.Album
	err := exec.GetContext(ctx, &album, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &album, nil
}

// GetByPublicID возвращает альбом по публичному идентификатору domain.PublicID.
// Если альбом отсутствует, возвращает domain.ErrNotFound.
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

	exec := r.db.executor(ctx)
	var album domain.Album
	err := exec.GetContext(ctx, &album, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &album, nil
}

// Update сохраняет редактируемые поля альбома.
// Если альбом отсутствует, возвращает domain.ErrNotFound.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *AlbumRepository) Update(ctx context.Context, album *domain.Album) error {
	const op = "album.Update"

	const query = `
		UPDATE albums 
		SET artist_id = :artist_id, title = :title, norm_title = :norm_title, 
			year = :year, release_date = :release_date, mbid = :mbid, lastfm_url = :lastfm_url, 
			cover_blob_id = :cover_blob_id, total_tracks = :total_tracks, is_complete = :is_complete, 
			enrich_status = :enrich_status, enriched_at = :enriched_at, updated_at = :updated_at
		WHERE public_id = :public_id`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, album)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return wrapDBError(op, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return wrapDBError(op, err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Delete удаляет запись альбома по publicID.
// Связанные треки сохраняются без привязки к этому альбому.
// Если альбом отсутствует, возвращает domain.ErrNotFound.
func (r *AlbumRepository) Delete(ctx context.Context, publicID domain.PublicID) error {
	const op = "album.Delete"

	const query = `DELETE FROM albums WHERE public_id = $1`

	exec := r.db.executor(ctx)
	result, err := exec.ExecContext(ctx, query, publicID)
	if err != nil {
		return wrapDBError(op, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return wrapDBError(op, err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// List возвращает альбомы, упорядоченные по NormTitle и ID по возрастанию.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
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
		ORDER BY norm_title, id
		LIMIT $1 OFFSET $2`

	exec := r.db.executor(ctx)
	var albums []domain.Album
	err := exec.SelectContext(ctx, &albums, query, limit, offset)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return albums, nil
}

// Search ищет альбомы по шаблону LIKE для полей Title и NormTitle.
// Результат упорядочен по NormTitle и ID по возрастанию.
// В pattern поддерживаются подстановочные знаки % и _.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
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
		WHERE title LIKE $1 OR norm_title LIKE $1 
		ORDER BY norm_title, id
		LIMIT $2 OFFSET $3`

	exec := r.db.executor(ctx)
	var albums []domain.Album
	err := exec.SelectContext(ctx, &albums, query, pattern, limit, offset)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return albums, nil
}
