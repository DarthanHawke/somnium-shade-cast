// Пакет sqlite реализовывает работу с базами данных
package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate доводит схему БД до актуальной версии
func (db *Database) Migrate(ctx context.Context) error {
	migrateFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: get sub fs: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db.DB.DB,
		migrateFS,
	)
	if err != nil {
		return fmt.Errorf("migrate: init provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}

	for _, r := range results {
		fmt.Printf("migration applied: %s (%s)\n", r.Source.Path, r.Duration)
	}
	return nil
}

// MigrationStatus — для админ-эндпоинта /admin/status и диагностики
func (db *Database) MigrationStatus(ctx context.Context) (current int64, err error) {
	migrateFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("migrate: get sub fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.DB.DB, migrateFS)
	if err != nil {
		return 0, fmt.Errorf("migrate: init provider: %w", err)
	}
	v, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: get version: %w", err)
	}
	return v, nil
}
