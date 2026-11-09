package sqlite

import (
	"context"
	"errors"
	"fmt"
)

// wrapDBError добавляет op к сообщению ошибки БД.
// Сохраняет context.Canceled и context.DeadlineExceeded для проверки через errors.Is,
// скрывая конкретный тип остальных ошибок. Для nil возвращает nil.
func wrapDBError(op string, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: %w", op, context.Canceled)

	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: %w", op, context.DeadlineExceeded)

	default:
		return fmt.Errorf("%s: %v", op, err)
	}
}

// isUniqueViolation сообщает, является ли err нарушением уникальности в SQLite.
func isUniqueViolation(err error) bool {
	// код 2067 = SQLITE_CONSTRAINT_UNIQUE, 1555 = SQLITE_CONSTRAINT_PRIMARYKEY
	type coder interface{ Code() int }
	var c coder
	if errors.As(err, &c) {
		return c.Code() == 2067 || c.Code() == 1555
	}
	return false
}
