package repo

import (
	"context"

	"github.com/uptrace/bun"
)

func queries(ctx context.Context, db *bun.DB) error {
	var users []User
	err := db.NewSelect().Model(&users).Where("active = ?", true).Limit(10).Scan(ctx)
	if err != nil {
		return err
	}

	u := &User{Name: "x"}
	_, err = db.NewInsert().Model(u).Exec(ctx)
	if err != nil {
		return err
	}

	_, err = db.NewUpdate().Model((*User)(nil)).Set("name = ?", "y").Where("id = ?", 1).Exec(ctx)
	if err != nil {
		return err
	}

	db.NewDelete().Model((*Story)(nil)).Where("id = ?", 1).Exec(ctx)
	db.NewCreateTable().Model((*User)(nil)).Exec(ctx)
	return nil
}
