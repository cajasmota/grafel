package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User is an ent schema definition. The embedded ent.Schema is the definitive
// marker; Fields() declares columns via field.<Type>("name") and Edges()
// declares relationships via edge.To/edge.From.
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("email").Unique(),
		field.Int("age"),
		field.Time("created_at"),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		// to-many: a user has many posts.
		edge.To("posts", Post.Type),
		// to-one: a user has one profile.
		edge.To("profile", Profile.Type).Unique(),
		// inverse to-one: a user belongs to one company.
		edge.From("company", Company.Type).Ref("users").Unique(),
	}
}
