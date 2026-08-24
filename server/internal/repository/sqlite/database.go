// Пакет database реализовывает работу с базами данных
package database

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
func New(dns string) (*Database, error) {
	// Открываем БД
	db, err := sqlx.Connect("sqlite", dns)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

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

	return &Database{db}, nil
}

// Close закрывает подключение к БД
func (db *Database) Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}
