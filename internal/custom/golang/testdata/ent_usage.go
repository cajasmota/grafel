package app

import "context"

// usage exercises the generated ent query builder and auto-migration so the
// extractor's Queries + Migrations recognition is covered. The `client` type
// is the generated *ent.Client; entity accessors are exported fields.
func usage(ctx context.Context, client *Client) error {
	// Auto-migration.
	if err := client.Schema.Create(ctx); err != nil {
		return err
	}

	// Typed query builder.
	client.User.Query().All(ctx)
	client.User.Create().SetName("alice").Save(ctx)
	client.Post.Update().Save(ctx)
	client.Profile.Delete().Exec(ctx)
	client.Company.Get(ctx, 1)
	return nil
}
