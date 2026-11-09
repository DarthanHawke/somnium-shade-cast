package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// BlobRepository хранит метаданные блобов и управляет числом ссылок на них в SQLite.
type BlobRepository struct {
	db *Database
}

// NewBlobRepository создаёт репозиторий, использующий db.
func NewBlobRepository(db *Database) *BlobRepository {
	return &BlobRepository{
		db: db,
	}
}

// Insert сохраняет метаданные блоба и при успехе заполняет blob.ID.
// Если исходный файл с таким SHA-256 уже зарегистрирован,
// возвращает domain.ErrBlobAlreadyExist. Сам файл метод не записывает.
func (r *BlobRepository) Insert(ctx context.Context, blob *domain.Blob) error {
	const op = "blobs.Insert"

	const query = `
		INSERT INTO blobs (sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at)
		VALUES (:sha256_plain, :size_plain, :size_encrypted, :mime_type, :kind,
			:enc_algo, :enc_key_wrapped, :master_key_id, :chunk_size,
			:storage_path, :ref_count, :created_at)`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, blob)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrBlobAlreadyExist
		}
		return wrapDBError(op, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return wrapDBError(op+": get inserted id", err)
	}
	blob.ID = domain.ID(id)
	return nil
}

// GetByID возвращает блоб по внутреннему идентификатору domain.ID.
// Если блоб отсутствует, возвращает domain.ErrNotFound.
func (r *BlobRepository) GetByID(ctx context.Context, id domain.ID) (*domain.Blob, error) {
	const op = "blobs.GetByID"

	const query = `
		SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at
		FROM blobs WHERE id = $1`

	exec := r.db.executor(ctx)
	var blob domain.Blob
	err := exec.GetContext(ctx, &blob, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &blob, nil
}

// GetBySHA256 возвращает метаданные блоба по SHA-256 исходного незашифрованного файла.
// Если блоб отсутствует, возвращает domain.ErrNotFound.
func (r *BlobRepository) GetBySHA256(ctx context.Context, shaPlain string) (*domain.Blob, error) {
	const op = "blobs.GetBySHA256"

	const query = `
		SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
			enc_algo, enc_key_wrapped, master_key_id, chunk_size,
			storage_path, ref_count, created_at
		FROM blobs WHERE sha256_plain = $1`

	exec := r.db.executor(ctx)
	var blob domain.Blob
	err := exec.GetContext(ctx, &blob, query, shaPlain)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &blob, nil
}

// IncRef увеличивает число ссылок на блоб на один.
// Если блоб отсутствует, возвращает domain.ErrNotFound.
func (r *BlobRepository) IncRef(ctx context.Context, id domain.ID) error {
	const op = "blobs.IncRef"

	const query = `UPDATE blobs SET ref_count = ref_count + 1 WHERE id = $1`

	exec := r.db.executor(ctx)
	result, err := exec.ExecContext(ctx, query, id)
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

// DecRef уменьшает число ссылок на блоб на один и возвращает новый счётчик.
// Возвращает domain.ErrNotFound, если блоб отсутствует,
// и domain.ErrAlreadyOrphan, если счётчик уже равен нулю.
// Для согласованного различения этих ошибок при конкурентных изменениях
// метод следует вызывать в транзакции.
func (r *BlobRepository) DecRef(
	ctx context.Context,
	id domain.ID,
) (int, error) {
	const op = "blobs.DecRef"
	const query = `
		UPDATE blobs
		SET ref_count = ref_count - 1
		WHERE id = $1 AND ref_count > 0
		RETURNING ref_count`

	exec := r.db.executor(ctx)

	var newCount int
	err := exec.GetContext(ctx, &newCount, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Отсутствие результата означает неизвестный ID или нулевой счётчик.
		// Проверяем наличие блоба, чтобы различить причины отказа.
		if _, err := r.GetByID(ctx, id); err != nil {
			return 0, err
		}
		return 0, domain.ErrAlreadyOrphan
	}
	if err != nil {
		return 0, wrapDBError(op, err)
	}

	return newCount, nil
}

// DeleteIfOrphan удаляет запись блоба с нулевым числом ссылок
// и возвращает путь к его файлу. Сам файл метод не удаляет.
// Возвращает domain.ErrNotFound, если блоб отсутствует,
// и domain.ErrBlobInUse, если блоб ещё используется.
// Для согласованного различения этих ошибок при конкурентных изменениях
// метод следует вызывать в транзакции.
// Возвращённый путь можно использовать для удаления файла после commit.
func (r *BlobRepository) DeleteIfOrphan(
	ctx context.Context,
	id domain.ID,
) (string, error) {
	const op = "blobs.DeleteIfOrphan"
	const query = `
		DELETE FROM blobs
		WHERE id = $1 AND ref_count = 0
		RETURNING storage_path`

	exec := r.db.executor(ctx)

	var storagePath string
	err := exec.GetContext(ctx, &storagePath, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Отсутствие результата означает неизвестный ID или нулевой счётчик.
		// Проверяем наличие блоба, чтобы различить причины отказа.
		if _, err := r.GetByID(ctx, id); err != nil {
			return "", err
		}
		return "", domain.ErrBlobInUse
	}
	if err != nil {
		return "", wrapDBError(op, err)
	}

	return storagePath, nil
}

// ListOrphans возвращает блобы с нулевым числом ссылок, созданные до createdBeforeMS.
// createdBeforeMS задаёт время Unix в миллисекундах.
// Результат упорядочен по CreatedAt по возрастанию.
func (r *BlobRepository) ListOrphans(ctx context.Context, createdBeforeMS int64) ([]*domain.Blob, error) {
	const op = "blobs.ListOrphans"
	const query = `
        SELECT id, sha256_plain, size_plain, size_encrypted, mime_type, kind,
            enc_algo, enc_key_wrapped, master_key_id, chunk_size,
            storage_path, ref_count, created_at
        FROM blobs
        WHERE ref_count = 0 AND created_at < $1
        ORDER BY created_at`

	exec := r.db.executor(ctx)
	var blobs []*domain.Blob
	err := exec.SelectContext(ctx, &blobs, query, createdBeforeMS)
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return blobs, nil
}

// PathExists сообщает, зарегистрирован ли в БД относительный путь rel.
// Наличие файла на диске метод не проверяет.
func (r *BlobRepository) PathExists(ctx context.Context, rel string) (bool, error) {
	const op = "blobs.PathExists"
	const query = `SELECT 1 FROM blobs WHERE storage_path = $1 LIMIT 1`

	var exists int
	exec := r.db.executor(ctx)
	err := exec.GetContext(ctx, &exists, query, rel)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, wrapDBError(op, err)
	}
	return true, nil
}
