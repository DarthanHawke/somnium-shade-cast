package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// TrackRepository реализует методы для работы с треками
type TrackRepository struct {
	db *Database
}

func NewTrackRepository(db *Database) *TrackRepository {
	return &TrackRepository{
		db: db,
	}
}

// Insert вставка новой муз композиции
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

	exec := r.db.ExecutorFromCtx(ctx)
	res, err := exec.NamedExecContext(ctx, query, track)
	if err != nil {
		return fmt.Errorf("track insert: %w", err)
	}
	id, _ := res.LastInsertId()
	track.ID = domain.ID(id)
	return nil
}

// GetByPublicID - возвращает трек по ULID
func (r *TrackRepository) GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.Track, error) {
	const op = "track.GetByPublicID"

	const query = `
		SELECT id, public_id, owner_user_id, album_id, artist_id,
			title, norm_title, track_no, disc_no, duration_ms,
			status, blob_id, fingerprint, fp_duration, is_intentional_dup,
			original_filename, codec, bitrate_kbps, sample_rate,
			mbid, lastfm_url, enrich_status, enriched_at, manual_override,
			created_at, updated_at 
		FROM tracks WHERE public_id = $1`

	exec := r.db.ExecutorFromCtx(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &track, nil
}

// GetBySHA256 - возвращает трек по sha хэшу
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

	exec := r.db.ExecutorFromCtx(ctx)
	var track domain.Track
	err := exec.GetContext(ctx, &track, query, sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &track, nil
}

// Update - обновляет данные трека
func (r *TrackRepository) Update(ctx context.Context, track *domain.Track) error {
	const op = "track.Update"

	const query = `
		UPDATE tracks
		SET album_id = :album_id, artist_id = :artist_id, title = :title, norm_title = :norm_title, 
			track_no = :track_no, disc_no = :disc_no, duration_ms = :duration_ms, fingerprint = :fingerprint, 
			fp_duration = :fp_duration, is_intentional_dup = :is_intentional_dup, 
			original_filename = :original_filename, codec = :codec, bitrate_kbps = :bitrate_kbps,
			sample_rate = :sample_rate, mbid = :mbid, lastfm_url = :lastfm_url, enrich_status = :enrich_status, 
			manual_override = :manual_override, updated_at = :updated_at
		WHERE public_id = :public_id`

	exec := r.db.ExecutorFromCtx(ctx)
	result, err := exec.NamedExecContext(ctx, query, track)
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

// Delete - хард удаление трека
func (r *TrackRepository) Delete(ctx context.Context, publicID domain.PublicID) error {
	const op = "track.Delete"

	const query = `DELETE FROM tracks WHERE public_id = $1`

	exec := r.db.ExecutorFromCtx(ctx)
	res, err := exec.ExecContext(ctx, query, publicID)
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

// Materialize - обновляет пустой трек(не загруженый - Ghost) после дозагрузки файла
func (r *TrackRepository) Materialize(ctx context.Context, track *domain.Track) error {
	const op = "track.Materialize"

	const query = `
		UPDATE tracks
		SET status = :status, blob_id = :blob_id, duration_ms = :duration_ms, codec = :codec, 
			bitrate_kbps = :bitrate_kbps, sample_rate = :sample_rate, updated_at = :updated_at
		WHERE public_id = :public_id`

	exec := r.db.ExecutorFromCtx(ctx)
	result, err := exec.NamedExecContext(ctx, query, track)
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
