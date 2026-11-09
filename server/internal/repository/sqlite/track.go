package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/jmoiron/sqlx"
)

// TrackRepository сохраняет и получает записи треков в SQLite.
type TrackRepository struct {
	db *Database
}

// NewTrackRepository создаёт репозиторий, использующий db.
func NewTrackRepository(db *Database) *TrackRepository {
	return &TrackRepository{
		db: db,
	}
}

const selectTrack = `
	SELECT id, public_id, owner_user_id, album_id, artist_id,
		title, norm_title, track_no, disc_no, duration_ms,
		status, blob_id, fingerprint, fp_duration, is_intentional_dup,
		original_filename, codec, bitrate_kbps, sample_rate,
		mbid, lastfm_url, enrich_status, enriched_at, manual_override,
		created_at, updated_at
	FROM tracks AS t
`

// readableTrackByPublicIDWhere задаёт правило чтения трека в пользовательских запросах.
// Алиас t обозначает tracks, $1 содержит publicID, $2 содержит viewerID.
const readableTrackByPublicIDWhere = `
	WHERE t.public_id = $1
	  AND t.owner_user_id = $2
`

// Insert сохраняет трек и при успехе заполняет track.ID.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *TrackRepository) Insert(ctx context.Context, track *domain.Track) error {
	const op = "track.Insert"

	const query = `
		INSERT INTO tracks (public_id, owner_user_id, album_id, artist_id,
			title, norm_title, track_no, disc_no, duration_ms,
			status, blob_id, fingerprint, fp_duration, is_intentional_dup,
			original_filename, codec, bitrate_kbps, sample_rate,
			mbid, lastfm_url, enrich_status, manual_override,
			created_at, updated_at)
		VALUES (:public_id, :owner_user_id, :album_id, :artist_id,
			:title, :norm_title, :track_no, :disc_no, :duration_ms,
			:status, :blob_id, :fingerprint, :fp_duration, :is_intentional_dup,
			:original_filename, :codec, :bitrate_kbps, :sample_rate,
			:mbid, :lastfm_url, :enrich_status, :manual_override,
			:created_at, :updated_at)`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, track)
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
	track.ID = domain.ID(id)
	return nil
}

// GetByPublicID возвращает запись трека по публичному идентификатору.
// Вызывающий код проверяет права на операцию с треком.
// Если трек отсутствует, возвращает domain.ErrNotFound.
func (r *TrackRepository) GetByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
) (*domain.Track, error) {
	const op = "track.GetByPublicID"
	const query = selectTrack + `WHERE t.public_id = $1`

	exec := r.db.executor(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &track, nil
}

// GetReadableByPublicID возвращает запись трека, доступного для чтения пользователю viewerID.
// Текущая политика разрешает чтение владельцу трека.
// Если трек отсутствует или недоступен, возвращает domain.ErrNotFound.
func (r *TrackRepository) GetReadableByPublicID(
	ctx context.Context,
	publicID domain.PublicID,
	viewerID domain.ID,
) (*domain.Track, error) {
	const op = "track.GetReadableByPublicID"
	const query = selectTrack + readableTrackByPublicIDWhere

	exec := r.db.executor(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, publicID, viewerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &track, nil
}

// GetBySHA256 возвращает один из треков, связанных с исходным файлом с SHA-256 sha.
// Поиск выполняется среди треков всех владельцев.
// При нескольких совпадениях выбор трека не определён.
// Если совпадений нет, возвращает domain.ErrNotFound.
func (r *TrackRepository) GetBySHA256(ctx context.Context, sha string) (*domain.Track, error) {
	const op = "track.GetBySHA256"

	const query = `
		SELECT id, public_id, owner_user_id, album_id, artist_id,
			title, norm_title, track_no, disc_no, duration_ms,
			status, blob_id, fingerprint, fp_duration, is_intentional_dup,
			original_filename, codec, bitrate_kbps, sample_rate,
			mbid, lastfm_url, enrich_status, enriched_at, manual_override,
			created_at, updated_at 
		FROM tracks 
		WHERE blob_id IN (SELECT id FROM blobs WHERE sha256_plain = $1)
		LIMIT 1`

	exec := r.db.executor(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &track, nil
}

// GetByOwnerAndSHA256 возвращает один из загруженных треков владельца ownerID,
// связанных с исходным файлом с SHA-256 sha.
// При нескольких совпадениях выбор трека не определён.
// Если совпадений нет, возвращает domain.ErrNotFound.
func (r *TrackRepository) GetByOwnerAndSHA256(
	ctx context.Context,
	sha string,
	ownerID domain.ID,
) (*domain.Track, error) {
	const op = "track.GetByOwnerAndSHA256"

	const query = `
		SELECT id, public_id, owner_user_id, album_id, artist_id,
			title, norm_title, track_no, disc_no, duration_ms,
			status, blob_id, fingerprint, fp_duration, is_intentional_dup,
			original_filename, codec, bitrate_kbps, sample_rate,
			mbid, lastfm_url, enrich_status, enriched_at, manual_override,
			created_at, updated_at 
		FROM tracks 
		WHERE owner_user_id = $1
			AND status = 'uploaded'
			AND blob_id IN (SELECT id FROM blobs WHERE sha256_plain = $2)
		LIMIT 1`

	exec := r.db.executor(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, ownerID, sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &track, nil
}

// Update сохраняет метаданные и технические характеристики трека по track.PublicID.
// Не изменяет статус трека, привязку блоба и EnrichedAt.
// Если трек отсутствует, возвращает domain.ErrNotFound.
func (r *TrackRepository) Update(ctx context.Context, track *domain.Track) error {
	const op = "track.Update"

	const query = `
		UPDATE tracks
		SET album_id = :album_id, artist_id = :artist_id, title = :title, 
			norm_title = :norm_title, track_no = :track_no, disc_no = :disc_no, 
			duration_ms = :duration_ms, fingerprint = :fingerprint, fp_duration = :fp_duration, 
			is_intentional_dup = :is_intentional_dup, original_filename = :original_filename, 
			codec = :codec, bitrate_kbps = :bitrate_kbps, sample_rate = :sample_rate, mbid = :mbid, 
			lastfm_url = :lastfm_url, enrich_status = :enrich_status, 
			manual_override = :manual_override, updated_at = :updated_at
		WHERE public_id = :public_id`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, track)
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

// Delete удаляет запись трека по publicID.
// Не удаляет файл блоба, не изменяет число ссылок на блоб и полнотекстовый индекс.
// Если трек отсутствует, возвращает domain.ErrNotFound.
func (r *TrackRepository) Delete(ctx context.Context, publicID domain.PublicID) error {
	const op = "track.Delete"

	const query = `DELETE FROM tracks WHERE public_id = $1`

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

// Materialize сохраняет данные загруженного файла в существующий ghost-трек.
// track должен содержать статус domain.TrackUploaded и BlobID загруженного файла.
// Название, исполнитель, альбом и нумерация трека остаются прежними.
// Возвращает domain.ErrConflict, если не найдена запись с указанными PublicID и OwnerID
// в статусе ghost и без привязанного блоба.
func (r *TrackRepository) Materialize(ctx context.Context, track *domain.Track) error {
	const op = "track.Materialize"

	const query = `
		UPDATE tracks
		SET original_filename = :original_filename, fingerprint = :fingerprint, 
			fp_duration = :fp_duration, status = :status, blob_id = :blob_id, 
			duration_ms = :duration_ms, codec = :codec, is_intentional_dup = :is_intentional_dup,
			bitrate_kbps = :bitrate_kbps, sample_rate = :sample_rate, updated_at = :updated_at
		WHERE public_id = :public_id
			AND owner_user_id = :owner_user_id
			AND status = 'ghost'
			AND blob_id IS NULL`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, track)
	if err != nil {
		return wrapDBError(op, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return wrapDBError(op, err)
	}
	if rowsAffected == 0 {
		return domain.ErrConflict
	}

	return nil
}

// ListByOwner возвращает треки владельца ownerID.
// Результат упорядочен по CreatedAt и ID по убыванию.
// offset задаёт число пропускаемых записей, limit — максимальное число результатов.
func (r *TrackRepository) ListByOwner(
	ctx context.Context,
	ownerID domain.ID,
	offset, limit int,
) ([]*domain.Track, error) {
	const op = "track.ListByOwner"
	const query = `
		SELECT id, public_id, owner_user_id, album_id, artist_id,
			title, norm_title, track_no, disc_no, duration_ms,
			status, blob_id, fingerprint, fp_duration, is_intentional_dup,
			original_filename, codec, bitrate_kbps, sample_rate,
			mbid, lastfm_url, enrich_status, enriched_at, manual_override,
			created_at, updated_at
		FROM tracks
		WHERE owner_user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`

	exec := r.db.executor(ctx)
	var tracks []*domain.Track
	err := exec.SelectContext(ctx, &tracks, query, ownerID, limit, offset)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return tracks, nil
}

// ArtistAlbumNames возвращает две мапы: ID исполнителя — имя и ID альбома — название.
// Отсутствующие в БД идентификаторы не добавляются в мапы.
func (r *TrackRepository) ArtistAlbumNames(
	ctx context.Context,
	artistIDs []domain.ID,
	albumIDs []domain.ID,
) (map[domain.ID]string, map[domain.ID]string, error) {
	const op = "track.ArtistAlbumNames"
	const artistsQuery = `
	SELECT id, name
	FROM artists
	WHERE id IN (?)`
	const albumsQuery = `
	SELECT id, title
	FROM albums
	WHERE id IN (?)`

	exec := r.db.executor(ctx)

	artists := make(map[domain.ID]string)
	albums := make(map[domain.ID]string)

	if len(artistIDs) > 0 {
		expandedQuery, args, err := sqlx.In(artistsQuery, artistIDs)
		if err != nil {
			return nil, nil, wrapDBError(op+": build artist query", err)
		}

		rows, err := exec.QueryxContext(ctx, expandedQuery, args...)
		if err != nil {
			return nil, nil, wrapDBError(op+": query artists", err)
		}
		for rows.Next() {
			var id domain.ID
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, nil, wrapDBError(op, err)
			}
			artists[id] = name
		}
		iterErr := rows.Err()
		closeErr := rows.Close()

		if iterErr != nil {
			return nil, nil, wrapDBError(op+": read artists", iterErr)
		}
		if closeErr != nil {
			return nil, nil, wrapDBError(op+": close artists", closeErr)
		}
	}

	if len(albumIDs) > 0 {
		expandedQuery, args, err := sqlx.In(albumsQuery, albumIDs)
		if err != nil {
			return nil, nil, wrapDBError(op+": build album query", err)
		}

		rows, err := exec.QueryxContext(ctx, expandedQuery, args...)
		if err != nil {
			return nil, nil, wrapDBError(op+": query albums", err)
		}
		for rows.Next() {
			var id domain.ID
			var title string
			if err := rows.Scan(&id, &title); err != nil {
				rows.Close()
				return nil, nil, wrapDBError(op, err)
			}
			albums[id] = title
		}
		iterErr := rows.Err()
		closeErr := rows.Close()

		if iterErr != nil {
			return nil, nil, wrapDBError(op+": read albums", iterErr)
		}
		if closeErr != nil {
			return nil, nil, wrapDBError(op+": close albums", closeErr)
		}
	}

	return artists, albums, nil
}
