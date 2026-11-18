package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// UserRepository сохраняет и получает записи пользователей в SQLite.
type UserRepository struct {
	db *Database
}

// NewUserRepository создаёт репозиторий, использующий db.
func NewUserRepository(db *Database) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

// Insert сохраняет пользователя и при успехе заполняет user.ID.
// При нарушении уникальности возвращает domain.ErrConflict.
func (r *UserRepository) Insert(ctx context.Context, user *domain.User) error {
	const op = "user.Insert"

	const query = `
		INSERT INTO users (public_id, name, display_name, role, avatar_blob_id, is_active, created_at)
		VALUES (:public_id, :name, :display_name, :role, :avatar_blob_id, :is_active, :created_at)`

	exec := r.db.executor(ctx)
	result, err := exec.NamedExecContext(ctx, query, user)
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
	user.ID = domain.ID(id)
	return nil
}

// GetByPublicID возвращает пользователя по публичному идентификатору domain.PublicID.
// Если пользователь отсутствует, возвращает domain.ErrNotFound.
func (r *UserRepository) GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.User, error) {
	const op = "user.GetByPublicID"

	const query = `
		SELECT id, public_id, name, display_name, role, avatar_blob_id, is_active, created_at
		FROM users WHERE public_id = $1`

	exec := r.db.executor(ctx)
	var user domain.User
	err := exec.GetContext(ctx, &user, query, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &user, nil
}

// List возвращает всех пользователей, включая неактивных, по возрастанию name.
// Если пользователей нет, возвращает пустой срез.
func (r *UserRepository) List(ctx context.Context) ([]domain.User, error) {
	const op = "user.List"

	const query = `
		SELECT id, public_id, name, display_name, role, avatar_blob_id, is_active, created_at
		FROM users
		ORDER BY name`

	users := make([]domain.User, 0)
	exec := r.db.executor(ctx)
	if err := exec.SelectContext(ctx, &users, query); err != nil {
		return nil, wrapDBError(op, err)
	}
	return users, nil
}

// Deactivate помечает пользователя с publicID как неактивного, сохраняя его запись.
// Если пользователь отсутствует, возвращает domain.ErrNotFound.
func (r *UserRepository) Deactivate(ctx context.Context, publicID domain.PublicID) error {
	const op = "user.Deactivate"

	const query = `
		UPDATE users 
		SET is_active = 0 
		WHERE public_id = $1`

	exec := r.db.executor(ctx)
	result, err := exec.ExecContext(ctx, query, string(publicID))
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

// Activate помечает пользователя с publicID как активного.
// Если пользователь отсутствует, возвращает domain.ErrNotFound.
func (r *UserRepository) Activate(ctx context.Context, publicID domain.PublicID) error {
	const op = "user.Activate"

	const query = `
		UPDATE users 
		SET is_active = 1 
		WHERE public_id = $1`

	exec := r.db.executor(ctx)
	result, err := exec.ExecContext(ctx, query, string(publicID))
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

// Delete удаляет пользователя и его треки с обновлением FTS и ссылок на блобы.
// Использует транзакцию из ctx или открывает свою. При отсутствии пользователя
// возвращает domain.ErrNotFound.
func (r *UserRepository) Delete(ctx context.Context, publicID domain.PublicID) error {
	const op = "user.Delete"
	const query = `DELETE FROM users WHERE public_id = $1`

	remove := func(ctx context.Context) error {
		user, err := r.GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		library := NewLibraryRepository(r.db)
		if _, err := library.removeTracks(ctx, user.ID, ""); err != nil {
			return err
		}

		result, err := r.db.executor(ctx).ExecContext(ctx, query, publicID)
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

	if txFromContext(ctx) != nil {
		return remove(ctx)
	}
	return NewTxRepository(r.db).WithTransaction(ctx, remove)
}

// EnsureAdmin создаёт администратора с именем name или возвращает существующую запись.
// Если имя занято обычным пользователем, возвращает domain.ErrConflict.
func (r *UserRepository) EnsureAdmin(ctx context.Context, name string) (*domain.User, error) {
	const op = "user.EnsureAdmin"
	const insert = `
		INSERT INTO users (public_id, name, display_name, role, is_active, created_at)
		VALUES (:public_id, :name, :display_name, :role, :is_active, :created_at)
		ON CONFLICT(name) DO NOTHING`
	user, err := domain.NewUser(name, "", domain.RoleAdmin)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	exec := r.db.executor(ctx)
	if _, err := exec.NamedExecContext(ctx, insert, user); err != nil {
		return nil, wrapDBError(op, err)
	}

	existing, err := r.GetByName(ctx, user.Name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if existing.Role != domain.RoleAdmin {
		return nil, fmt.Errorf("%s: name %q is already occupied: %w", op, existing.Name, domain.ErrConflict)
	}
	return existing, nil
}

// GetByName возвращает пользователя по имени. Если пользователь отсутствует,
// возвращает domain.ErrNotFound.
func (r *UserRepository) GetByName(ctx context.Context, name string) (*domain.User, error) {
	const op = "user.GetByName"
	const query = `
		SELECT id, public_id, name, display_name, role, avatar_blob_id, is_active, created_at
		FROM users WHERE name = $1`

	var user domain.User
	err := r.db.executor(ctx).GetContext(ctx, &user, query, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrapDBError(op, err)
	}
	return &user, nil
}
