package models

import (
	"time"

	"github.com/uptrace/bun"
)

// User maps to the "users" table. The table name is declared on the embedded
// bun.BaseModel via the bun:"table:...,alias:..." tag; scalar fields carry
// column tags; relationship fields carry rel: tags.
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID        int64     `bun:"id,pk,autoincrement"`
	Name      string    `bun:"name,notnull"`
	Email     string    `bun:"email_address,unique"`
	CompanyID int64     `bun:"company_id"`
	CreatedAt time.Time `bun:"created_at"`

	// belongs-to: a user belongs to one company.
	Company *Company `bun:"rel:belongs-to,join:company_id=id"`
	// has-many: a user has many orders.
	Orders []*Order `bun:"rel:has-many,join:id=user_id"`
	// has-one: a user has one profile.
	Profile *Profile `bun:"rel:has-one,join:id=user_id"`
	// m2m: a user has many roles through user_roles.
	Roles []*Role `bun:"m2m:user_roles,join:User=Role"`
}

// Company has no explicit table tag (bun infers "companies").
type Company struct {
	bun.BaseModel

	ID   int64  `bun:"id,pk"`
	Name string `bun:"name"`
}

type Order struct {
	bun.BaseModel `bun:"table:orders"`

	ID     int64   `bun:"id,pk,autoincrement"`
	UserID int64   `bun:"user_id"`
	Total  float64 `bun:"total"`
}

type Profile struct {
	bun.BaseModel `bun:"table:profiles"`

	ID     int64  `bun:"id,pk"`
	UserID int64  `bun:"user_id"`
	Bio    string `bun:"bio"`
}

type Role struct {
	bun.BaseModel `bun:"table:roles"`

	ID   int64  `bun:"id,pk"`
	Name string `bun:"name"`
}
