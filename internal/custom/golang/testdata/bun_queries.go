package repo

import (
	"context"

	"github.com/uptrace/bun"
)

// queries exercises the bun fluent query builder (model-bound and free) plus
// DDL builders, covering Queries + part of Migrations recognition.
func queries(ctx context.Context, db *bun.DB) error {
	users := []User{}

	// Model-bound select via &T{} form.
	db.NewSelect().Model(&User{}).Where("id = ?", 1).Scan(ctx)
	// Model-bound select via (*T)(nil) form.
	db.NewSelect().Model((*Order)(nil)).Scan(ctx, &users)
	// Insert / Update / Delete (literal model forms bind statically).
	db.NewInsert().Model(&User{}).Exec(ctx)
	db.NewUpdate().Model((*User)(nil)).WherePK().Exec(ctx)
	db.NewDelete().Model(&User{}).Where("id = ?", 1).Exec(ctx)
	// Raw query (unbound).
	db.NewRaw("SELECT 1").Scan(ctx)

	// DDL as migration.
	db.NewCreateTable().Model((*User)(nil)).Exec(ctx)
	db.NewDropTable().Model((*Order)(nil)).Exec(ctx)
	return nil
}
