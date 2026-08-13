package infrastructure

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func MigrateUp(ctx context.Context, dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func MigrateDown(ctx context.Context, dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	source, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return m, nil
}
