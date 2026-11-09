package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// ArtistRepository сохраняет и получает данные исполнителей в SQLite.
type ArtistRepository struct {
	db *Database
}

// NewArtistRepository создаёт репозиторий, использующий db.
func NewArtistRepository(db *Database) *ArtistRepository {
	return &ArtistRepository{
		db: db,
	}
}

// Insert сохраняет исполнителя и при успехе заполняет artist.ID.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *ArtistRepository) Insert(ctx context.Context, artist *domain.Artist) error {
	const op = "artist.Insert"

	const query = `
		INSERT INTO artists (public_id, name, norm_name, sort_name, mbid, lastfm_url, 
					image_blob_id, bio, enrich_status, enriched_at, created_at, updated_at)
		VALUES (:public_id, :name, :norm_name, :sort_name, :mbid, :lastfm_url, 
					:image_blob_id, :bio, :enrich_status, :enriched_at, :created_at, :updated_at)`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, artist)
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
	artist.ID = domain.ID(id)
	return nil
}

// GetByNormName возвращает исполнителя по нормализованному имени normName.
// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
func (r *ArtistRepository) GetByNormName(ctx context.Context, normName string) (*domain.Artist, error) {
	const op = "artist.GetByNormName"
	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url,
			image_blob_id, bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists WHERE norm_name = $1`

	exec := r.db.executor(ctx)
	var artist domain.Artist
	err := exec.GetContext(ctx, &artist, query, normName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &artist, nil
}

// GetByID возвращает исполнителя по внутреннему идентификатору domain.ID.
// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
func (r *ArtistRepository) GetByID(
	ctx context.Context,
	id domain.ID,
) (*domain.Artist, error) {
	const op = "artist.GetByID"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists WHERE id = $1`

	exec := r.db.executor(ctx)
	var artist domain.Artist
	err := exec.GetContext(ctx, &artist, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &artist, nil
}

// GetByPublicID возвращает исполнителя по публичному идентификатору domain.PublicID.
// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
func (r *ArtistRepository) GetByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
) (*domain.Artist, error) {
	const op = "artist.GetByPublicID"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists WHERE public_id = $1`

	exec := r.db.executor(ctx)
	var artist domain.Artist
	err := exec.GetContext(ctx, &artist, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &artist, nil
}

// Update сохраняет редактируемые поля исполнителя.
// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *ArtistRepository) Update(ctx context.Context, artist *domain.Artist) error {
	const op = "artist.Update"

	const query = `
		UPDATE artists 
		SET name = :name, norm_name = :norm_name, sort_name = :sort_name, mbid = :mbid, 
			lastfm_url = :lastfm_url, image_blob_id = :image_blob_id, bio = :bio, 
			enrich_status = :enrich_status, enriched_at = :enriched_at, updated_at = :updated_at
		WHERE public_id = :public_id`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, artist)
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

// Delete удаляет запись исполнителя по publicID.
// Его альбомы и треки удаляются каскадно согласно схеме БД.
// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
func (r *ArtistRepository) Delete(ctx context.Context, publicID domain.PublicID) error {
	const op = "artist.Delete"

	const query = `DELETE FROM artists WHERE public_id = $1`

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

// List возвращает исполнителей, упорядоченных по SortName и ID по возрастанию.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
func (r *ArtistRepository) List(
	ctx context.Context,
	offset, limit int,
) ([]domain.Artist, error) {
	const op = "artist.List"

	const query = `
		SELECT id, public_id, name, norm_name, sort_name, mbid, lastfm_url, image_blob_id, 
					bio, enrich_status, enriched_at, created_at, updated_at
		FROM artists 
		ORDER BY sort_name, id
		LIMIT $1 OFFSET $2`

	exec := r.db.executor(ctx)
	var artists []domain.Artist
	err := exec.SelectContext(ctx, &artists, query, limit, offset)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return artists, nil
}

// Search ищет исполнителей по шаблону LIKE для полей Name и SortName.
// Результат упорядочен по SortName и ID по возрастанию.
// В pattern поддерживаются подстановочные знаки % и _.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
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
		ORDER BY sort_name, id
		LIMIT $2 OFFSET $3`

	exec := r.db.executor(ctx)
	var artists []domain.Artist
	err := exec.SelectContext(ctx, &artists, query, pattern, limit, offset)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return artists, nil
}
