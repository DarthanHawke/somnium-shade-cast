// Пакет sqlite реализовывает работу с sqlite
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	"fmt"

	_ "modernc.org/sqlite"
)

// Executor - общий интерфейс для sqlx.db и sqlx.tx
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryxContext(ctx context.Context, query string, args ...any) (*sqlx.Rows, error)
	QueryRowxContext(ctx context.Context, query string, args ...any) *sqlx.Row
	GetContext(ctx context.Context, dest any, query string, args ...any) error
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
	NamedExecContext(ctx context.Context, query string, arg any) (sql.Result, error)
}

// Database - обёртка для sqlx.DB
type Database struct {
	*sqlx.DB
}

// New создаёт новое подключение к БД
func Open(dsn string) (*Database, error) {
	// Открываем БД
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Параметры соединения
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Проверяем соединение
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Используем Write-Ahead Logging для сохранения изменений перед записью в бд
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable Write-Ahead Logging: %v", err)
	}
	// Включаем проверку внешних ключей для согласованности данных
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %v", err)
	}
	// Добавляем паузы с ретраем при блокировке бд
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("failed to enable busy timeout: %v", err)
	}
	// Т.к. используем WAL, то для целостности хватит нормал
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return nil, fmt.Errorf("failed to enable normal synchronous: %v", err)
	}

	return &Database{db}, nil
}

// Close закрывает подключение к БД
func (db *Database) Close() error {
	if db != nil && db.DB != nil {
		return db.DB.Close() // Закрываем базовый sqlx.DB
	}
	return nil
}

type txKey struct{}

// withTx кладёт транзакцию в контекст
func withTx(ctx context.Context, tx *sqlx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// txFromCtx достаёт транзакцию из контекста (или nil)
func txFromCtx(ctx context.Context) *sqlx.Tx {
	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}
	return nil
}

// ExecutorFromCtx возвращает транзакцию, если она есть в контексте,
// иначе - обычное соединение
func (db *Database) ExecutorFromCtx(ctx context.Context) Executor {
	if tx := txFromCtx(ctx); tx != nil {
		return tx
	}
	return db.DB
}

// WithTransaction выполняет функцию в транзакции.
// Автоматически коммитит при успехе, откат при ошибке.
func (db *Database) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	// Если уже внутри транзакции - просто выполняем fn с тем же ctx
	if txFromCtx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil { // перехватываем panic
			_ = tx.Rollback()
			panic(p) // пробрасываем panic дальше после отката
		}
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// isUniqueViolation - возвращает ошибки с кодами SQLite
func isUniqueViolation(err error) bool {
	// код 2067 = SQLITE_CONSTRAINT_UNIQUE, 1555 = SQLITE_CONSTRAINT_PRIMARYKEY
	type coder interface{ Code() int }
	var c coder
	if errors.As(err, &c) {
		return c.Code() == 2067 || c.Code() == 1555
	}
	return false
}
