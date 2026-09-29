package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobRepository реализует методы для работы с блобсами
type BlobRepository struct {
	db *Database
}

func NewBlobRepository(db *Database) *BlobRepository {
	return &BlobRepository{
		db: db,
	}
}

// Insert вставка нового blob
func (r *BlobRepository) Insert(ctx context.Context, blob *domain.Blob) error {
	const op = "blobs.Insert"

	const query = `
		INSERT INTO blobs (sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at)
		VALUES (:sha256_plain, :size_plain, :size_encrypted, :mime_type, :kind,
			:enc_algo, :enc_key_wrapped, :master_key_id, :chunk_size,
			:storage_path, :ref_count, :created_at)`

	exec := r.db.ExecutorFromCtx(ctx)
	res, err := exec.NamedExecContext(ctx, query, blob)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	id, err := res.LastInsertId()
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	blob.ID = domain.ID(id)
	return nil
}

// GetByID - возвращает блоб по ID
func (r *BlobRepository) GetByID(ctx context.Context, id domain.ID) (*domain.Blob, error) {
	const op = "blobs.GetByID"

	const query = `
		SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at
		FROM blobs WHERE id = $1`

	exec := r.db.ExecutorFromCtx(ctx)
	var blob domain.Blob
	err := exec.GetContext(ctx, &blob, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &blob, nil
}

// GetBySHA256 - возвращает блоб по sha хэшу
func (r *BlobRepository) GetBySHA256(ctx context.Context, shaPlain string) (*domain.Blob, error) {
	const op = "blobs.GetBySHA256"

	const query = `
		SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at
		FROM blobs WHERE sha256_plain = $1`

	exec := r.db.ExecutorFromCtx(ctx)
	var blob domain.Blob
	err := exec.GetContext(ctx, &blob, query, shaPlain)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &blob, nil
}

// IncRef увеличивает счетчик
func (r *BlobRepository) IncRef(ctx context.Context, id domain.ID) error {
	const op = "blobs.IncRef"

	const query = `UPDATE blobs SET ref_count = ref_count + 1 WHERE id = $1`

	exec := r.db.ExecutorFromCtx(ctx)
	res, err := exec.ExecContext(ctx, query, id)
	if err != nil {
		return &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DecRef уменьшает счётчик
func (r *BlobRepository) DecRef(ctx context.Context, id domain.ID) (newCount int, err error) {
	const op = "blobs.DecRef"

	const query = `UPDATE blobs SET ref_count = ref_count - 1 WHERE id = $1 AND ref_count > 0`

	exec := r.db.ExecutorFromCtx(ctx)
	res, err := exec.ExecContext(ctx, query, id)
	if err != nil {
		return 0, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, domain.ErrConflict
	}

	err = exec.QueryRowxContext(ctx,
		`SELECT ref_count FROM blobs WHERE id = $1`, id).Scan(&newCount)
	if err != nil {
		return 0, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return newCount, nil
}

// DeleteIfOrphan удаляет строку, если ref_count = 0
// Возвращает storage_path удалённого блоба для удаления файла с диска,
// или ErrConflict, если blob ещё используется
func (r *BlobRepository) DeleteIfOrphan(ctx context.Context, id domain.ID) (storagePath string, err error) {
	const op = "blobs.DeleteIfOrphan"

	exec := r.db.ExecutorFromCtx(ctx)
	err = exec.QueryRowxContext(ctx,
		`SELECT storage_path FROM blobs WHERE id = $1 AND ref_count = 0`, id).
		Scan(&storagePath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrConflict
	}
	if err != nil {
		return "", &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM blobs WHERE id = $1`, id); err != nil {
		return "", &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return storagePath, nil
}

// ListOrphans - возвращает список строк у которых ref_count = 0
func (r *BlobRepository) ListOrphans(ctx context.Context, olderThanUnixMillis int64) ([]*domain.Blob, error) {
	const op = "blobs.ListOrphans"
	const query = `
        SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
            enc_algo, enc_key_wrapped, master_key_id, chunk_size,
            storage_path, ref_count, created_at
        FROM blobs
        WHERE ref_count = 0 AND created_at < $1
        ORDER BY created_at`

	exec := r.db.ExecutorFromCtx(ctx)
	var blobs []*domain.Blob
	err := exec.SelectContext(ctx, &blobs, query, olderThanUnixMillis)
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return blobs, nil
}

// PathExists - для storage.GC (интерфейс BlobPathChecker)
func (r *BlobRepository) PathExists(ctx context.Context, rel string) (bool, error) {
	const op = "blobs.PathExists"
	const query = `SELECT 1 FROM blobs WHERE storage_path = $1 LIMIT 1`

	var exists int
	exec := r.db.ExecutorFromCtx(ctx)
	err := exec.GetContext(ctx, &exists, query, rel)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return true, nil
}
