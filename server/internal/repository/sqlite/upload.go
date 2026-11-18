package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// UploadRepository связывает принятые файлы с задачами и результатами загрузки.
type UploadRepository struct {
	db *Database
}

// NewUploadRepository создаёт репозиторий загрузки.
func NewUploadRepository(db *Database) *UploadRepository {
	return &UploadRepository{db: db}
}

const uploadSelect = `
	SELECT u.public_id, u.owner_user_id, u.idempotency_key, u.request_hash, u.blob_id,
	       u.job_id, u.original_filename, u.force_dup, u.ghost_public_id, u.fingerprint,
	       u.fp_duration, u.track_public_id, u.error_code, u.created_at, u.completed_at,
	       CASE
	           WHEN u.track_public_id IS NOT NULL THEN 'ready'
	           WHEN u.error_code IS NOT NULL THEN 'failed'
	           WHEN j.status = 'running' AND j.lease_until > ` + jobNow + ` THEN 'processing'
	           ELSE 'queued'
	       END AS status
	FROM track_uploads AS u LEFT JOIN jobs AS j ON j.id = u.job_id `

// ByKey возвращает ранее принятый импорт того же пользователя.
func (r *UploadRepository) ByKey(ctx context.Context, owner domain.ID, key string) (*domain.TrackUpload, error) {
	return r.get(ctx, "uploads.ByKey", uploadSelect+"WHERE u.owner_user_id = ?1 AND u.idempotency_key = ?2", owner, key)
}

// ForOwner возвращает импорт только его владельцу.
func (r *UploadRepository) ForOwner(ctx context.Context, owner domain.ID, id domain.PublicID) (*domain.TrackUpload, error) {
	return r.get(ctx, "uploads.ForOwner", uploadSelect+"WHERE u.owner_user_id = ?1 AND u.public_id = ?2", owner, id)
}

// ByPublicID возвращает импорт для фонового обработчика.
func (r *UploadRepository) ByPublicID(ctx context.Context, id domain.PublicID) (*domain.TrackUpload, error) {
	return r.get(ctx, "uploads.ByPublicID", uploadSelect+"WHERE u.public_id = ?1", id)
}

// ListForOwner возвращает страницу истории импортов от новых к старым.
func (r *UploadRepository) ListForOwner(ctx context.Context, owner domain.ID, limit, offset int) ([]domain.TrackUpload, error) {
	const query = uploadSelect + `
		WHERE u.owner_user_id = ?1 ORDER BY u.created_at DESC, u.public_id DESC LIMIT ?2 OFFSET ?3`
	var items []domain.TrackUpload
	err := r.db.executor(ctx).SelectContext(ctx, &items, query, owner, limit, offset)
	return items, wrapDBError("uploads.ListForOwner", err)
}

func (r *UploadRepository) get(ctx context.Context, op, query string, args ...any) (*domain.TrackUpload, error) {
	var result domain.TrackUpload
	err := r.db.executor(ctx).GetContext(ctx, &result, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &result, nil
}

// Insert регистрирует принятый файл. Вызывается в общей транзакции с постановкой задачи.
func (r *UploadRepository) Insert(ctx context.Context, item *domain.TrackUpload) error {
	const query = `
		INSERT INTO track_uploads (public_id, owner_user_id, idempotency_key, request_hash, blob_id,
		    job_id, original_filename, force_dup, ghost_public_id, fingerprint, fp_duration, created_at)
		VALUES (:public_id, :owner_user_id, :idempotency_key, :request_hash, :blob_id,
		    :job_id, :original_filename, :force_dup, :ghost_public_id, :fingerprint, :fp_duration, :created_at)`
	_, err := r.db.executor(ctx).NamedExecContext(ctx, query, item)
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}
	return wrapDBError("uploads.Insert", err)
}

// LockAttempt проверяет право текущей попытки завершить импорт. UPDATE получает
// блокировку записи до работы с треком, чтобы аренда не могла смениться до commit.
func (r *UploadRepository) LockAttempt(ctx context.Context, id domain.PublicID, job domain.Job) (bool, error) {
	const query = `
		UPDATE track_uploads SET job_id = job_id
		WHERE public_id = ?1 AND job_id = ?2
		  AND completed_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM jobs WHERE id = ?2 AND status = 'running'
		        AND lease_token = ?3 AND lease_until > ` + jobNow + `
		  )`
	return execAffected(ctx, r.db.executor(ctx), "uploads.LockAttempt", query, id, job.ID, job.LeaseToken)
}

// Finish сохраняет итог импорта и снимает удержание файла. Вызывается после LockAttempt.
func (r *UploadRepository) Finish(ctx context.Context, id domain.PublicID, track *domain.PublicID, errorCode *string) error {
	const query = `
		UPDATE track_uploads
		SET track_public_id = ?2, error_code = ?3, completed_at = ` + jobNow + `,
		    blob_id = NULL, job_id = NULL
		WHERE public_id = ?1`
	_, err := r.db.executor(ctx).ExecContext(ctx, query, id, track, errorCode)
	return wrapDBError("uploads.Finish", err)
}
