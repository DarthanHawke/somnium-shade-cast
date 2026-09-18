// Пакет sqlite реализовывает работу с sqlite
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/jmoiron/sqlx"
)

// UserRepository реализует методы для работы с пользователями
type UserRepository struct {
	db *Database
}

func NewUserRepository(db *Database) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

// Create - создает нового пользователя
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	const op = "user.Create"

	const query = `
		INSERT INTO users (public_id, name, display_name, role, avatar_blob_id, is_active, created_at)
		VALUES (:public_id, :name, :display_name, :role, :avatar_blob_id, :is_active, :created_at)`

	res, err := r.db.NamedExecContext(ctx, query, user)
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
	user.ID = domain.NewID(id)
	return nil
}

// GetByPublicID - возвращает пользователя по ULID
func (r *UserRepository) GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error) {
	const op = "user.GetByPublicID"

	const query = `
		SELECT id, public_id, name, display_name, role, avatar_blob_id, is_active, created_at
		FROM users WHERE public_id = $1`

	var user domain.User
	err := r.db.GetContext(ctx, &user, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, &domain.WrappedError{
			Op:  op,
			Err: err,
		}
	}
	return &user, nil
}

// Deactivate - деактивирует пользователя (soft delete)
func (r *UserRepository) Deactivate(ctx context.Context, publicID domain.PublicID) error {
	const op = "user.Deactivate"

	const query = `
		UPDATE users 
		SET is_active = 0 
		WHERE public_id = $1`

	result, err := r.db.ExecContext(ctx, query, string(publicID))
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

// Activate - активирует пользователя
func (r *UserRepository) Activate(ctx context.Context, publicID domain.PublicID) error {
	const op = "user.Activate"

	const query = `
		UPDATE users 
		SET is_active = 1 
		WHERE public_id = $1`

	result, err := r.db.ExecContext(ctx, query, string(publicID))
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

// Delete - хард удаление пользователя
func (r *UserRepository) Delete(ctx context.Context, tx *sqlx.Tx, publicID domain.PublicID) error {
	const op = "user.Delete"

	const query = `DELETE FROM users WHERE public_id = $1`

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
