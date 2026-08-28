// Пакет sqlite реализовывает работу с sqlite
package sqlite

import (
	"github.com/jmoiron/sqlx"

	"fmt"

	_ "modernc.org/sqlite"
)

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
