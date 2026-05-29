package migrations

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// Migrations registers bun migrations via the migrate API, covering the
// migrate-API branch of Migrations recognition.
var Migrations = migrate.NewMigrations()

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewCreateTable().Model((*User)(nil)).Exec(ctx)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewDropTable().Model((*User)(nil)).Exec(ctx)
		return err
	})
}

func run(ctx context.Context, db *bun.DB) error {
	migrator := migrate.NewMigrator(db, Migrations)
	if err := migrator.Init(ctx); err != nil {
		return err
	}
	_, err := migrator.Migrate(ctx)
	return err
}
