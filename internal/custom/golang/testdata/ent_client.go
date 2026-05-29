package main

import (
	"context"

	"entgo.io/ent/dialect/sql/schema"
)

func run(ctx context.Context, client *Client) error {
	// Auto-migration entry point.
	if err := client.Schema.Create(ctx, schema.WithDropIndex(true), migrate.WithForeignKeys(true)); err != nil {
		return err
	}

	// Typed query builder usage.
	u, err := client.User.Query().Where(user.NameEQ("a")).Order(ent.Asc("name")).Limit(10).All(ctx)
	_ = u
	if err != nil {
		return err
	}

	_, err = client.User.Create().SetName("bob").Save(ctx)
	if err != nil {
		return err
	}

	client.Pet.Delete().Where(pet.IDEQ(1)).Exec(ctx)
	client.Group.Get(ctx, 1)
	return nil
}
