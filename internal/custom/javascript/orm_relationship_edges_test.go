package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// graphRelatesEdge returns the first GRAPH_RELATES edge (across all entities)
// whose FromID/ToID match the given model refs, or nil.
func graphRelatesEdge(ents []types.EntityRecord, from, to string) *types.RelationshipRecord {
	for ei := range ents {
		for ri := range ents[ei].Relationships {
			r := &ents[ei].Relationships[ri]
			if r.Kind == string(types.RelationshipKindGraphRelates) &&
				r.FromID == from && r.ToID == to {
				return r
			}
		}
	}
	return nil
}

func extractEnts(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func assertCardinality(t *testing.T, r *types.RelationshipRecord, from, to, card string) {
	t.Helper()
	if r == nil {
		t.Fatalf("expected GRAPH_RELATES %s → %s, not found", from, to)
	}
	if got := r.Properties["cardinality"]; got != card {
		t.Errorf("cardinality: want %q, got %q (props=%v)", card, got, r.Properties)
	}
}

// ---------------------------------------------------------------------------
// Sequelize
// ---------------------------------------------------------------------------

func TestSequelizeGraphRelatesEdges(t *testing.T) {
	src := `
const User = sequelize.define('User', {});
const Order = sequelize.define('Order', {});
const Profile = sequelize.define('Profile', {});
const Role = sequelize.define('Role', {});

User.hasMany(Order);
Order.belongsTo(User);
User.hasOne(Profile);
User.belongsToMany(Role);
`
	ents := extractEnts(t, "custom_js_sequelize", fi("models.js", "javascript", src))

	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Order"), "Class:User", "Class:Order", "one_to_many")
	assertCardinality(t, graphRelatesEdge(ents, "Class:Order", "Class:User"), "Class:Order", "Class:User", "many_to_one")
	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Profile"), "Class:User", "Class:Profile", "one_to_one")
	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Role"), "Class:User", "Class:Role", "many_to_many")
}

// ---------------------------------------------------------------------------
// TypeORM
// ---------------------------------------------------------------------------

func TestTypeORMGraphRelatesEdges(t *testing.T) {
	src := `
@Entity()
export class User {
  @OneToMany(() => Order, order => order.user)
  orders: Order[];

  @OneToOne(() => Profile)
  profile: Profile;
}

@Entity()
export class Order {
  @ManyToOne(() => User, user => user.orders)
  user: User;
}

@Entity()
export class Profile {}
`
	ents := extractEnts(t, "custom_js_typeorm", fi("entities.ts", "typescript", src))

	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Order"), "Class:User", "Class:Order", "one_to_many")
	assertCardinality(t, graphRelatesEdge(ents, "Class:Order", "Class:User"), "Class:Order", "Class:User", "many_to_one")
	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Profile"), "Class:User", "Class:Profile", "one_to_one")
}

// Cross-file / unresolved target: @ManyToMany(() => Tag) where Tag is not a
// same-file @Entity must NOT fabricate a model node (no edge).
func TestTypeORMUnresolvedTargetNoEdge(t *testing.T) {
	src := `
@Entity()
export class Post {
  @ManyToMany(() => Tag)
  tags: Tag[];
}
`
	ents := extractEnts(t, "custom_js_typeorm", fi("post.ts", "typescript", src))
	if e := graphRelatesEdge(ents, "Class:Post", "Class:Tag"); e != nil {
		t.Errorf("expected NO GRAPH_RELATES edge for cross-file Tag target, got %v", e)
	}
	// But the topology must still be preserved as a prop on the relation entity.
	found := false
	for _, ent := range ents {
		if ent.Subtype == "relation" && ent.Properties["target_entity"] == "Tag" {
			found = true
		}
	}
	if !found {
		t.Error("expected relation entity carrying target_entity=Tag (honest-partial)")
	}
}

// ---------------------------------------------------------------------------
// Prisma
// ---------------------------------------------------------------------------

func TestPrismaGraphRelatesEdges(t *testing.T) {
	src := `
model User {
  id      Int     @id @default(autoincrement())
  orders  Order[]
  profile Profile?
}

model Order {
  id      Int   @id
  userId  Int
  user    User  @relation(fields: [userId], references: [id])
}

model Profile {
  id      Int   @id
  userId  Int   @unique
  user    User  @relation(fields: [userId], references: [id])
}
`
	ents := extractEnts(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))

	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Order"), "Class:User", "Class:Order", "one_to_many")
	assertCardinality(t, graphRelatesEdge(ents, "Class:Order", "Class:User"), "Class:Order", "Class:User", "many_to_one")
	// User.profile Profile? is the scalar back side → one_to_one.
	assertCardinality(t, graphRelatesEdge(ents, "Class:User", "Class:Profile"), "Class:User", "Class:Profile", "one_to_one")
}

// Negative: a scalar (non-model) field type must not produce an edge.
func TestPrismaScalarFieldNoEdge(t *testing.T) {
	src := `
model User {
  id    Int     @id
  name  String
  age   Int
}
`
	ents := extractEnts(t, "custom_js_prisma", fi("schema.prisma", "prisma", src))
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == string(types.RelationshipKindGraphRelates) {
				t.Errorf("unexpected GRAPH_RELATES edge for scalar-only model: %v", r)
			}
		}
	}
}
