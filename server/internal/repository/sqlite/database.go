// Package sqlite реализует хранение пользователей, музыкальной библиотеки
// и метаданных блобов в SQLite.
// Репозитории используют транзакцию из контекста, если она задана.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// executor объединяет операции sqlx.DB и sqlx.Tx, необходимые репозиториям.
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryxContext(ctx context.Context, query string, args ...any) (*sqlx.Rows, error)
	QueryRowxContext(ctx context.Context, query string, args ...any) *sqlx.Row
	GetContext(ctx context.Context, dest any, query string, args ...any) error
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
	NamedExecContext(ctx context.Context, query string, arg any) (sql.Result, error)
}

// Database управляет пулом соединений с SQLite и предоставляет операции миграции схемы.
type Database struct {
	*sqlx.DB
}

// Open открывает БД по dsn и настраивает соединения для работы с SQLite.
// Включает WAL и проверку внешних ключей.
// При ошибке инициализации закрывает созданный пул соединений.
// После использования возвращённую БД следует закрыть вызовом Close.
func Open(dsn string) (*Database, error) {
	conn, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db := &Database{DB: conn}

	if err := db.initialize(); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf(
				"close database after initialization failure: %w",
				closeErr,
			)
		}

		return nil, errors.Join(err, closeErr)
	}

	return db, nil
}

// initialize проверяет подключение и задаёт параметры соединений SQLite.
func (db *Database) initialize() error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Настраиваем ожидание блокировок до остальных PRAGMA.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("enable Write-Ahead Logging: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("set synchronous mode: %w", err)
	}

	return nil
}

// Close закрывает пул соединений с БД.
// Для nil или неинициализированного Database возвращает nil.
func (db *Database) Close() error {
	if db != nil && db.DB != nil {
		return db.DB.Close() // Закрываем базовый sqlx.DB
	}
	return nil
}

// executor возвращает транзакцию из ctx, если она задана,
// или пул соединений db.DB.
func (db *Database) executor(ctx context.Context) executor {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return db.DB
}
