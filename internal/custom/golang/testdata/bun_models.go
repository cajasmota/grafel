package models

import (
	"time"

	"github.com/uptrace/bun"
)

// User is a bun model with an explicit table mapping.
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID        int64     `bun:"id,pk,autoincrement"`
	Name      string    `bun:"name,notnull"`
	Email     string    `bun:"email,unique"`
	CreatedAt time.Time `bun:"created_at"`

	// has-many relationship.
	Stories []*Story `bun:"rel:has-many,join:id=author_id"`
	// belongs-to relationship.
	Profile *Profile `bun:"rel:belongs-to,join:profile_id=id"`
	// many-to-many relationship through a join table.
	Roles []*Role `bun:"m2m:user_roles,join:User=Role"`
}

// Story is a plain bun model (tags only, no explicit table tag).
type Story struct {
	bun.BaseModel `bun:"table:stories"`

	ID       int64  `bun:"id,pk"`
	Title    string `bun:"title"`
	AuthorID int64  `bun:"author_id"`
}

// Profile carries bun field tags without a BaseModel embed.
type Profile struct {
	ID  int64  `bun:"id,pk"`
	Bio string `bun:"bio"`
}
