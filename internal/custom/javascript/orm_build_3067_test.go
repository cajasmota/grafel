package javascript_test

// Tests for issue #3067: ORM extractor builds — Prisma @relation,
// Sequelize hasMany/belongsTo/schema, Drizzle references(), MikroORM decorators.
// Covers: schema_extraction, association_extraction, foreign_key_extraction,
//         relationship_extraction for lang.jsts.orm.{prisma,sequelize,drizzle,mikro-orm}.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Prisma — @relation, model field schema, relationship extraction
// ---------------------------------------------------------------------------

func TestPrismaRelationDirective(t *testing.T) {
	src := `
model Post {
  id       Int    @id
  authorId Int
  author   User   @relation(fields: [authorId], references: [id])
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation subtype entity from @relation directive")
	}
}

func TestPrismaRelationNamedFK(t *testing.T) {
	src := `
model Post {
  id       Int    @id @default(autoincrement())
  authorId Int
  author   User   @relation(fields: [authorId], references: [id])
  tagId    Int
  tag      Tag    @relation("PostTag", fields: [tagId], references: [id])
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity for named @relation")
	}
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity for @relation fields/references")
	}
}

func TestPrismaSchemaFieldExtraction(t *testing.T) {
	src := `
model User {
  id    Int     @id @default(autoincrement())
  name  String
  email String  @unique
  posts Post[]
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	// Should emit a schema-aggregate entity for the model
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema entity")
	}
	// Should emit field entities for typed fields
	if !containsSubtype(ents, "field") {
		t.Error("expected field entities from Prisma model definition")
	}
}

func TestPrismaRelationshipExtraction(t *testing.T) {
	src := `
model User {
  id    Int    @id
  posts Post[]
}
model Post {
  id       Int    @id
  authorId Int
  author   User   @relation(fields: [authorId], references: [id])
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity for Prisma @relation (relationship_extraction)")
	}
}

func TestPrismaForeignKeyExtraction(t *testing.T) {
	src := `
model Order {
  id         Int      @id
  customerId Int
  customer   Customer @relation(fields: [customerId], references: [id])
}
`
	ents := extract(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity for Prisma @relation fields/references")
	}
}

// ---------------------------------------------------------------------------
// Sequelize — schema_extraction (DataTypes column defs) + associations
// ---------------------------------------------------------------------------

func TestSequelizeSchemaColumnExtraction(t *testing.T) {
	src := `
const User = sequelize.define('User', {
  id: {
    type: DataTypes.INTEGER,
    primaryKey: true,
    autoIncrement: true,
  },
  name: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  email: {
    type: DataTypes.STRING,
    unique: true,
  },
})
`
	ents := extract(t, "custom_js_sequelize", fi("user.model.ts", "typescript", src))
	if !containsSubtype(ents, "column") {
		t.Error("expected column entity from Sequelize DataTypes field definition")
	}
}

func TestSequelizeSchemaColumnClassModel(t *testing.T) {
	src := `
class Product extends Model {}
Product.init({
  id: { type: DataTypes.INTEGER, primaryKey: true },
  title: { type: DataTypes.STRING },
  price: { type: DataTypes.DECIMAL(10, 2) },
}, { sequelize })
`
	ents := extract(t, "custom_js_sequelize", fi("product.model.ts", "typescript", src))
	if !containsSubtype(ents, "column") {
		t.Error("expected column entities from Sequelize Model.init DataTypes")
	}
}

func TestSequelizeAssociationHasMany(t *testing.T) {
	src := `User.hasMany(Post, { foreignKey: 'authorId' })`
	ents := extract(t, "custom_js_sequelize", fi("associations.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "User.hasMany(Post)") {
		t.Error("expected User.hasMany(Post) association entity")
	}
}

func TestSequelizeAssociationBelongsTo(t *testing.T) {
	src := `Post.belongsTo(User, { foreignKey: 'authorId', as: 'author' })`
	ents := extract(t, "custom_js_sequelize", fi("associations.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Post.belongsTo(User)") {
		t.Error("expected Post.belongsTo(User) association entity")
	}
}

func TestSequelizeAssociationBelongsToMany(t *testing.T) {
	src := `
Post.belongsToMany(Tag, { through: 'PostTag' })
Tag.belongsToMany(Post, { through: 'PostTag' })
`
	ents := extract(t, "custom_js_sequelize", fi("associations.ts", "typescript", src))
	if !containsSubtype(ents, "association") {
		t.Error("expected association entity for belongsToMany")
	}
}

func TestSequelizeForeignKeyColumn(t *testing.T) {
	src := `
const Post = sequelize.define('Post', {
  authorId: {
    type: DataTypes.INTEGER,
    references: { model: 'Users', key: 'id' },
  },
})
`
	ents := extract(t, "custom_js_sequelize", fi("post.model.ts", "typescript", src))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity for Sequelize references field")
	}
}

// ---------------------------------------------------------------------------
// Drizzle — references() FK column + relations() relationship
// ---------------------------------------------------------------------------

func TestDrizzleReferencesFK(t *testing.T) {
	src := `
import { pgTable, serial, integer, text } from 'drizzle-orm/pg-core'

export const posts = pgTable('posts', {
  id: serial('id').primaryKey(),
  authorId: integer('author_id').references(() => users.id),
  title: text('title').notNull(),
})
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity from Drizzle .references()")
	}
}

func TestDrizzleReferencesOnDeleteCascade(t *testing.T) {
	src := `
export const comments = pgTable('comments', {
  id: serial('id').primaryKey(),
  postId: integer('post_id').references(() => posts.id, { onDelete: 'cascade' }),
})
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	if !containsSubtype(ents, "foreign_key") {
		t.Error("expected foreign_key entity from Drizzle .references() with onDelete option")
	}
}

func TestDrizzleRelationsFunction(t *testing.T) {
	src := `
import { relations } from 'drizzle-orm'

export const usersRelations = relations(users, ({ many }) => ({
  posts: many(posts),
}))

export const postsRelations = relations(posts, ({ one }) => ({
  author: one(users, { fields: [posts.authorId], references: [users.id] }),
}))
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity from Drizzle relations() function")
	}
}

func TestDrizzleSchemaColumnExtraction(t *testing.T) {
	src := `
import { pgTable, serial, text, integer } from 'drizzle-orm/pg-core'

export const users = pgTable('users', {
  id: serial('id').primaryKey(),
  name: text('name').notNull(),
  age: integer('age'),
})
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	// The table entity itself proves schema_extraction
	if !containsEntity(ents, "SCOPE.Schema", "users") {
		t.Error("expected users schema entity from pgTable definition")
	}
	if !containsSubtype(ents, "column") {
		t.Error("expected column entities from Drizzle table column definitions")
	}
}

func TestDrizzleAssociationFromRelations(t *testing.T) {
	src := `
export const postRelations = relations(posts, ({ one, many }) => ({
  author: one(users, { fields: [posts.userId], references: [users.id] }),
  comments: many(comments),
}))
`
	ents := extract(t, "custom_js_drizzle", fi("schema.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity for Drizzle one()/many() relations (association_extraction)")
	}
}

// ---------------------------------------------------------------------------
// MikroORM — @Entity schema, @ManyToOne/@OneToMany associations, FKs
// ---------------------------------------------------------------------------

func TestMikroORMEntitySchemaExtraction(t *testing.T) {
	src := `
import { Entity, Property, PrimaryKey } from '@mikro-orm/core'

@Entity()
export class User {
  @PrimaryKey()
  id!: number

  @Property()
  name!: string

  @Property()
  email!: string
}
`
	ents := extract(t, "custom_js_mikroorm", fi("user.entity.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User schema entity from @Entity decorator")
	}
	if !containsSubtype(ents, "field") {
		t.Error("expected field entities from MikroORM @Property decorators")
	}
}

func TestMikroORMManyToOneRelation(t *testing.T) {
	src := `
import { Entity, ManyToOne, OneToMany, Collection } from '@mikro-orm/core'

@Entity()
export class Post {
  @ManyToOne(() => User)
  author!: User
}
`
	ents := extract(t, "custom_js_mikroorm", fi("post.entity.ts", "typescript", src))
	if !containsEntity(ents, "SCOPE.Component", "ManyToOne:author") {
		t.Error("expected ManyToOne:author relation entity")
	}
}

func TestMikroORMOneToManyRelation(t *testing.T) {
	src := `
@Entity()
export class User {
  @OneToMany(() => Post, post => post.author)
  posts = new Collection<Post>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("user.entity.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity from @OneToMany decorator")
	}
}

func TestMikroORMManyToManyRelation(t *testing.T) {
	src := `
@Entity()
export class Post {
  @ManyToMany(() => Tag)
  tags = new Collection<Tag>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("post.entity.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity from @ManyToMany decorator")
	}
}

func TestMikroORMForeignKeyProperty(t *testing.T) {
	src := `
@Entity()
export class Post {
  @ManyToOne()
  author!: User

  @Property({ columnType: 'int' })
  authorId!: number
}
`
	ents := extract(t, "custom_js_mikroorm", fi("post.entity.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entity for MikroORM @ManyToOne (foreign_key_extraction)")
	}
}

func TestMikroORMAssociationExtraction(t *testing.T) {
	src := `
@Entity()
export class User {
  @OneToMany(() => Post, p => p.author)
  posts = new Collection<Post>(this)

  @ManyToMany(() => Role, role => role.users, { owner: true })
  roles = new Collection<Role>(this)
}
`
	ents := extract(t, "custom_js_mikroorm", fi("user.entity.ts", "typescript", src))
	if !containsSubtype(ents, "relation") {
		t.Error("expected relation entities from MikroORM @OneToMany and @ManyToMany (association_extraction)")
	}
}
