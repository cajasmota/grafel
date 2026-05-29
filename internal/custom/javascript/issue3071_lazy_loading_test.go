package javascript_test

// Issue #3071 — lazy_loading_recognition builds for TypeORM / Sequelize /
// MikroORM and not_applicable evidence for Knex / Objection.

import "testing"

// ---------------------------------------------------------------------------
// TypeORM
// ---------------------------------------------------------------------------

// TestTypeORM_LazyRelationExplicitOption verifies that a relation decorator
// carrying { lazy: true } is detected and emitted as a lazy_relation entity.
func TestTypeORM_LazyRelationExplicitOption(t *testing.T) {
	src := `
import { Entity, OneToMany } from "typeorm"

@Entity()
export class User {
  @OneToMany(() => Post, post => post.user, { lazy: true })
  posts: Promise<Post[]>
}
`
	ents := extract(t, "custom_js_typeorm", fi("user.entity.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "lazy_relation" && e.Name == "lazy:OneToMany:posts" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCOPE.Pattern/lazy_relation entity 'lazy:OneToMany:posts' for TypeORM { lazy: true } relation")
	}
}

// TestTypeORM_LazyManyToOne verifies lazy detection on @ManyToOne.
func TestTypeORM_LazyManyToOne(t *testing.T) {
	src := `
@Entity()
export class Post {
  @ManyToOne(() => User, user => user.posts, { lazy: true })
  author: Promise<User>
}
`
	ents := extract(t, "custom_js_typeorm", fi("post.entity.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "lazy_relation" && e.Name == "lazy:ManyToOne:author" {
			found = true
		}
	}
	if !found {
		t.Error("expected lazy_relation entity 'lazy:ManyToOne:author' for TypeORM ManyToOne { lazy: true }")
	}
}

// TestTypeORM_NonLazyRelationNotEmittedAsLazy ensures a relation without
// lazy: true is not emitted as a lazy_relation.
func TestTypeORM_NonLazyRelationNotEmittedAsLazy(t *testing.T) {
	src := `
@Entity()
export class Order {
  @OneToMany(() => Item, item => item.order)
  items: Item[]
}
`
	ents := extract(t, "custom_js_typeorm", fi("order.entity.ts", "typescript", src))
	for _, e := range ents {
		if e.Subtype == "lazy_relation" {
			t.Errorf("unexpected lazy_relation entity %q — no lazy: true was present", e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Sequelize
// ---------------------------------------------------------------------------

// TestSequelize_LazyAssociation verifies that a hasMany call with { lazy: true }
// is detected and emitted as a lazy_association entity.
func TestSequelize_LazyAssociation(t *testing.T) {
	src := `
User.hasMany(Post, { foreignKey: 'userId', lazy: true })
`
	ents := extract(t, "custom_js_sequelize", fi("associations.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "lazy_association" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCOPE.Pattern/lazy_association for Sequelize hasMany with { lazy: true }")
	}
}

// TestSequelize_LazyBelongsTo verifies that a belongsTo call with { lazy: true }
// is detected.
func TestSequelize_LazyBelongsTo(t *testing.T) {
	src := `
Post.belongsTo(User, { lazy: true })
`
	ents := extract(t, "custom_js_sequelize", fi("assoc.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "lazy_association" {
			found = true
		}
	}
	if !found {
		t.Error("expected lazy_association for Sequelize belongsTo with { lazy: true }")
	}
}

// TestSequelize_NonLazyAssocNotEmittedAsLazy ensures an association without
// lazy: true is not emitted as lazy_association.
func TestSequelize_NonLazyAssocNotEmittedAsLazy(t *testing.T) {
	src := `
User.hasMany(Comment, { foreignKey: 'userId' })
`
	ents := extract(t, "custom_js_sequelize", fi("assoc.ts", "typescript", src))
	for _, e := range ents {
		if e.Subtype == "lazy_association" {
			t.Errorf("unexpected lazy_association entity %q — no lazy: true present", e.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// MikroORM
// ---------------------------------------------------------------------------

// TestMikroORM_LazyRelationExplicitOption verifies that a relation decorator
// with { lazy: true } is detected and emitted as a lazy_relation entity.
func TestMikroORM_LazyRelationExplicitOption(t *testing.T) {
	src := `
import { Entity, OneToMany } from "@mikro-orm/core"

@Entity()
export class Author {
  @OneToMany(() => Book, book => book.author, { lazy: true })
  books = new Collection<Book>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("author.entity.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "lazy_relation" && e.Name == "lazy:OneToMany:books" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCOPE.Pattern/lazy_relation 'lazy:OneToMany:books' for MikroORM { lazy: true } relation")
	}
}

// TestMikroORM_LazyRelationLoadStrategy verifies that a relation using
// LoadStrategy.LAZY is also detected.
func TestMikroORM_LazyRelationLoadStrategy(t *testing.T) {
	src := `
@Entity()
export class Publisher {
  @OneToMany(() => Book, book => book.publisher, { strategy: LoadStrategy.LAZY })
  books = new Collection<Book>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("publisher.entity.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "lazy_relation" && e.Name == "lazy:OneToMany:books" {
			found = true
		}
	}
	if !found {
		t.Error("expected lazy_relation 'lazy:OneToMany:books' for MikroORM LoadStrategy.LAZY")
	}
}

// TestMikroORM_LazyRelationExtraLazy verifies that LoadStrategy.EXTRA_LAZY is
// recognised and the lazy_loading property is set to "extra_lazy".
func TestMikroORM_LazyRelationExtraLazy(t *testing.T) {
	src := `
@Entity()
export class Category {
  @ManyToMany(() => Tag, tag => tag.categories, { strategy: LoadStrategy.EXTRA_LAZY })
  tags = new Collection<Tag>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("category.entity.ts", "typescript", src))
	found := false
	for _, e := range ents {
		if e.Subtype == "lazy_relation" && e.Name == "lazy:ManyToMany:tags" {
			found = true
		}
	}
	if !found {
		t.Error("expected lazy_relation 'lazy:ManyToMany:tags' for MikroORM LoadStrategy.EXTRA_LAZY")
	}
}

// TestMikroORM_NonLazyRelationNotEmittedAsLazy ensures a relation without
// lazy options is not emitted as lazy_relation.
func TestMikroORM_NonLazyRelationNotEmittedAsLazy(t *testing.T) {
	src := `
@Entity()
export class Product {
  @ManyToOne(() => Category, cat => cat.products)
  category: Category
}
`
	ents := extract(t, "custom_js_mikroorm", fi("product.entity.ts", "typescript", src))
	for _, e := range ents {
		if e.Subtype == "lazy_relation" {
			t.Errorf("unexpected lazy_relation entity %q — no lazy options present", e.Name)
		}
	}
}
