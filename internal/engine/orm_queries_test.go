// Tests for the applyORMQueries pass (#723).
//
// Strategy: build a small in-memory file, run the detector, then assert
// on the QUERIES edges emitted in DetectResult.Relationships. The
// canonical caller-side / model-side IDs are documented in orm_queries.go;
// the assertions here use those shapes verbatim so a regression in the
// ID convention surfaces as a test failure.
//
// One test per ORM × at least two query shapes (simple + complex).
// Cross-language parity: a TestORM_NonORMFileNoChange test asserts that
// files containing no ORM call sites emit zero QUERIES edges. This is
// the byte-identical-on-non-ORM check called out by the acceptance
// criteria.
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// detectORM is the test helper: run the full detector pipeline against
// `content` for `lang` and return only the QUERIES edges. Drops the
// surrounding rule-driven entities + non-QUERIES edges so the assertion
// shape stays small.
func detectORM(t *testing.T, lang, path, content string) []ormEdge {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var out []ormEdge
	for _, r := range res.Relationships {
		if r.Kind != ormQueriesEdgeKind {
			continue
		}
		out = append(out, ormEdge{
			From:       r.FromID,
			To:         r.ToID,
			Op:         r.Properties.Get("operation"),
			ORM:        r.Properties.Get("orm"),
			IsJoin:     r.Properties.Get("is_join"),
			FilterKeys: r.Properties.Get("filter_keys"),
		})
	}
	return out
}

type ormEdge struct {
	From       string
	To         string
	Op         string
	ORM        string
	IsJoin     string
	FilterKeys string
}

// assertEdgeExists is the workhorse assertion: returns the matching edge
// if found, fails the test otherwise.
func assertEdgeExists(t *testing.T, edges []ormEdge, from, to, op string) ormEdge {
	t.Helper()
	for _, e := range edges {
		if e.From == from && e.To == to && e.Op == op {
			return e
		}
	}
	t.Errorf("missing QUERIES edge from=%q to=%q op=%q\n  got: %+v", from, to, op, edges)
	return ormEdge{}
}

// ---------------------------------------------------------------------------
// Python: Django ORM
// ---------------------------------------------------------------------------

func TestORM_DjangoSimpleAndComplex(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    name = models.CharField(max_length=100)

def get_user(user_id):
    return User.objects.get(id=user_id)

def list_active_users():
    return User.objects.filter(is_active=True, role__name="admin").select_related("profile")

def create_user(name):
    return User.objects.create(name=name)

def remove_user(user_id):
    User.objects.filter(id=user_id).delete()
`
	edges := detectORM(t, "python", "app/views.py", src)

	// Simple find: get_user → User, op=find.
	e := assertEdgeExists(t, edges, "Function:get_user", "Class:User", "find")
	if e.ORM != "django" {
		t.Errorf("expected orm=django, got %q", e.ORM)
	}
	if !strings.Contains(e.FilterKeys, "id") {
		t.Errorf("expected filter_keys to contain id, got %q", e.FilterKeys)
	}

	// Complex (is_join): list_active_users → User. The Django matcher
	// flags `role__name` and `.select_related(...)` as relationship
	// traversals.
	e2 := assertEdgeExists(t, edges, "Function:list_active_users", "Class:User", "find")
	if e2.IsJoin != "true" {
		t.Errorf("expected is_join=true for select_related/__ traversal, got %q", e2.IsJoin)
	}

	// Create + delete: ensure all four CRUD verbs land on User.
	assertEdgeExists(t, edges, "Function:create_user", "Class:User", "create")
	assertEdgeExists(t, edges, "Function:remove_user", "Class:User", "delete")
}

// ---------------------------------------------------------------------------
// Python: SQLAlchemy (classic + 2.0)
// ---------------------------------------------------------------------------

func TestORM_SQLAlchemyClassicAnd2x(t *testing.T) {
	src := `from sqlalchemy import select
from sqlalchemy.orm import joinedload

class Post:
    pass

def get_posts(session, author_id):
    return session.query(Post).filter(author_id=author_id).all()

async def get_post_with_author(session, post_id):
    stmt = select(Post).where(id=post_id).options(joinedload(Post.author))
    return await session.execute(stmt)
`
	edges := detectORM(t, "python", "app/repo.py", src)
	e := assertEdgeExists(t, edges, "Function:get_posts", "Class:Post", "find")
	if e.ORM != "sqlalchemy" {
		t.Errorf("expected orm=sqlalchemy, got %q", e.ORM)
	}

	e2 := assertEdgeExists(t, edges, "Function:get_post_with_author", "Class:Post", "find")
	if e2.IsJoin != "true" {
		t.Errorf("expected is_join=true for joinedload, got %q", e2.IsJoin)
	}
}

// ---------------------------------------------------------------------------
// Python: Beanie (async MongoDB ODM) — #3645 sibling parity
// ---------------------------------------------------------------------------

func TestORM_BeanieDocumentQueries(t *testing.T) {
	src := `from beanie import Document

class User(Document):
    name: str
    age: int

async def get_user(user_id):
    return await User.get(user_id)

async def list_adults():
    return await User.find(User.age >= 18).to_list()

async def find_one_user(name):
    return await User.find_one(User.name == name)

async def add_users(users):
    return await User.insert_many(users)

async def remove_user(user_id):
    await User.find_one(User.id == user_id).delete()

async def report():
    return await User.aggregate([{"$group": {"_id": "$age"}}]).to_list()
`
	edges := detectORM(t, "python", "app/repo.py", src)

	// find: get_user resolves to op=find, orm=beanie (the .get verb).
	e := assertEdgeExists(t, edges, "Function:get_user", "Class:User", "find")
	if e.ORM != "beanie" {
		t.Errorf("expected orm=beanie, got %q", e.ORM)
	}

	// find: list_adults via User.find(...).
	assertEdgeExists(t, edges, "Function:list_adults", "Class:User", "find")
	// find: find_one_user via User.find_one(...).
	assertEdgeExists(t, edges, "Function:find_one_user", "Class:User", "find")
	// create: insert_many flattens to create.
	ec := assertEdgeExists(t, edges, "Function:add_users", "Class:User", "create")
	if ec.ORM != "beanie" {
		t.Errorf("expected orm=beanie for insert_many, got %q", ec.ORM)
	}
	// aggregate: pipeline call flattens to aggregate.
	assertEdgeExists(t, edges, "Function:report", "Class:User", "aggregate")
}

// Negative: a Beanie-shaped call in a file that does NOT import beanie must
// not fabricate a QUERIES edge — the matcher is import-gated.
func TestORM_BeanieNotImportedNoEdge(t *testing.T) {
	src := `class Helper:
    pass

def run():
    # Looks Beanie-ish but no beanie import: must stay silent.
    return Helper.find(x=1)
`
	edges := detectORM(t, "python", "app/util.py", src)
	for _, e := range edges {
		if e.ORM == "beanie" {
			t.Errorf("did not expect a beanie edge without import, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Python: MongoEngine (sync MongoDB ODM) — #3645 sibling parity
// ---------------------------------------------------------------------------

func TestORM_MongoEngineDirectAndChained(t *testing.T) {
	src := `from mongoengine import Document, StringField

class Article(Document):
    title = StringField()

def find_by_title(title):
    # Direct manager-call form Django's matcher never covers.
    return Article.objects(title=title)

def filter_published():
    return Article.objects.filter(published=True).order_by("-created")

def get_one(article_id):
    return Article.objects.get(id=article_id)

def purge():
    Article.objects.filter(stale=True).delete()
`
	edges := detectORM(t, "python", "app/articles.py", src)

	// Direct-call form: Article.objects(title=...) → find, orm=mongoengine,
	// with the kwarg captured as a filter key.
	e := assertEdgeExists(t, edges, "Function:find_by_title", "Class:Article", "find")
	if e.ORM != "mongoengine" {
		t.Errorf("expected orm=mongoengine, got %q", e.ORM)
	}
	if !strings.Contains(e.FilterKeys, "title") {
		t.Errorf("expected filter_keys to contain title, got %q", e.FilterKeys)
	}

	// Chained verb forms.
	ef := assertEdgeExists(t, edges, "Function:filter_published", "Class:Article", "find")
	if ef.ORM != "mongoengine" {
		t.Errorf("expected orm=mongoengine for .objects.filter, got %q", ef.ORM)
	}
	assertEdgeExists(t, edges, "Function:get_one", "Class:Article", "find")
	assertEdgeExists(t, edges, "Function:purge", "Class:Article", "delete")

	// Exclusivity: a mongoengine-only file must NOT also emit an orm=django
	// edge for the same `.objects.<verb>` call site (no double-emit /
	// mis-attribution).
	for _, ed := range edges {
		if ed.ORM == "django" {
			t.Errorf("mongoengine-only file should not emit a django edge, got %+v", ed)
		}
	}
}

// Negative: a MongoEngine-shaped call in a file that does NOT import
// mongoengine must not fabricate a mongoengine edge. (It may still match the
// Django matcher if `.objects.<verb>` is present, which is correct Django
// behaviour — we only assert no mongoengine edge here.)
func TestORM_MongoEngineNotImportedNoEdge(t *testing.T) {
	src := `def run():
    return Article.objects(title="x")
`
	edges := detectORM(t, "python", "app/util.py", src)
	for _, e := range edges {
		if e.ORM == "mongoengine" {
			t.Errorf("did not expect a mongoengine edge without import, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// JS/TS: Prisma
// ---------------------------------------------------------------------------

func TestORM_PrismaSimpleAndJoin(t *testing.T) {
	src := `import { prisma } from './client'

export async function getUser(id: string) {
  return prisma.user.findUnique({ where: { id } })
}

export async function listPostsWithAuthor() {
  return prisma.post.findMany({
    where: { published: true },
    include: { author: true },
  })
}

export async function createPost(data) {
  return prisma.post.create({ data })
}

export async function deletePost(id) {
  return prisma.post.delete({ where: { id } })
}
`
	edges := detectORM(t, "typescript", "src/posts.ts", src)
	e := assertEdgeExists(t, edges, "Function:getUser", "Class:User", "find")
	if e.ORM != "prisma" {
		t.Errorf("expected orm=prisma, got %q", e.ORM)
	}

	e2 := assertEdgeExists(t, edges, "Function:listPostsWithAuthor", "Class:Post", "find")
	if e2.IsJoin != "true" {
		t.Errorf("expected is_join=true for include:, got %q", e2.IsJoin)
	}

	assertEdgeExists(t, edges, "Function:createPost", "Class:Post", "create")
	assertEdgeExists(t, edges, "Function:deletePost", "Class:Post", "delete")
}

// ---------------------------------------------------------------------------
// JS/TS: Supabase
// ---------------------------------------------------------------------------

func TestORM_SupabaseTableName(t *testing.T) {
	src := "import { supabase } from './client'\n" +
		"\n" +
		"export async function listProducts() {\n" +
		"  return supabase.from('products').select('*')\n" +
		"}\n" +
		"\n" +
		"export async function addProduct(p) {\n" +
		"  return supabase.from('products').insert(p)\n" +
		"}\n"
	edges := detectORM(t, "typescript", "src/products.ts", src)
	e := assertEdgeExists(t, edges, "Function:listProducts", "Class:Product", "find")
	if e.ORM != "supabase" {
		t.Errorf("expected orm=supabase, got %q", e.ORM)
	}
	assertEdgeExists(t, edges, "Function:addProduct", "Class:Product", "create")
}

// ---------------------------------------------------------------------------
// Java: JPA + Spring Data
// ---------------------------------------------------------------------------

func TestORM_JavaJPAAndSpringData(t *testing.T) {
	src := `package app;

public class UserService {
    public User getUser(Long id) {
        return entityManager.find(User.class, id);
    }

    public User findByEmail(String email) {
        return userRepository.findByEmail(email);
    }

    public void save(User u) {
        userRepository.save(u);
    }
}
`
	edges := detectORM(t, "java", "src/UserService.java", src)
	// JPA: entityManager.find(User.class, id) → find on User
	e := assertEdgeExists(t, edges, "Function:getUser", "Class:User", "find")
	if e.ORM != "jpa" {
		t.Errorf("expected orm=jpa, got %q", e.ORM)
	}

	// Spring Data: userRepository.findByEmail(...)
	e2 := assertEdgeExists(t, edges, "Function:findByEmail", "Class:User", "find")
	if e2.ORM != "spring_data" {
		t.Errorf("expected orm=spring_data, got %q", e2.ORM)
	}
}

// ---------------------------------------------------------------------------
// Go: gorm
// ---------------------------------------------------------------------------

func TestORM_GoGorm(t *testing.T) {
	src := `package repo

func GetUser(db *gorm.DB, id uint) (*User, error) {
    var user User
    err := db.First(&user, id)
    return &user, err
}

func ListUsersWithPosts(db *gorm.DB) []User {
    var users []User
    db.Preload("Posts").Find(&users)
    return users
}

func CreateUser(db *gorm.DB, user *User) error {
    return db.Create(&user).Error
}
`
	edges := detectORM(t, "go", "repo/user.go", src)
	e := assertEdgeExists(t, edges, "Function:GetUser", "Class:User", "find")
	if e.ORM != "gorm" {
		t.Errorf("expected orm=gorm, got %q", e.ORM)
	}

	e2 := assertEdgeExists(t, edges, "Function:ListUsersWithPosts", "Class:User", "find")
	if e2.IsJoin != "true" {
		t.Errorf("expected is_join=true for Preload, got %q", e2.IsJoin)
	}

	assertEdgeExists(t, edges, "Function:CreateUser", "Class:User", "create")
}

// ---------------------------------------------------------------------------
// Ruby: ActiveRecord
// ---------------------------------------------------------------------------

func TestORM_RubyActiveRecord(t *testing.T) {
	src := `class UsersController < ApplicationController
  def show
    @user = User.find(params[:id])
  end

  def index
    @users = User.where(active: true).includes(:posts)
  end

  def create
    @user = User.create(user_params)
  end
end
`
	edges := detectORM(t, "ruby", "app/controllers/users_controller.rb", src)
	e := assertEdgeExists(t, edges, "Function:show", "Class:User", "find")
	if e.ORM != "activerecord" {
		t.Errorf("expected orm=activerecord, got %q", e.ORM)
	}

	e2 := assertEdgeExists(t, edges, "Function:index", "Class:User", "find")
	if e2.IsJoin != "true" {
		t.Errorf("expected is_join=true for .includes, got %q", e2.IsJoin)
	}

	assertEdgeExists(t, edges, "Function:create", "Class:User", "create")
}

// ---------------------------------------------------------------------------
// Cross-language parity: files without ORM calls emit no QUERIES edges.
// ---------------------------------------------------------------------------

func TestORM_NoEdgesOnNonORMFile(t *testing.T) {
	cases := []struct {
		lang string
		path string
		src  string
	}{
		{"python", "app/util.py", "def add(a, b):\n    return a + b\n"},
		{"javascript", "src/util.js", "function add(a, b) { return a + b }\n"},
		{"typescript", "src/util.ts", "export const add = (a: number, b: number) => a + b\n"},
		{"go", "util.go", "package util\nfunc Add(a, b int) int { return a + b }\n"},
		{"java", "Util.java", "public class Util { public int add(int a, int b) { return a + b; } }\n"},
		{"ruby", "util.rb", "def add(a, b)\n  a + b\nend\n"},
		{"csharp", "Util.cs", "public class Util { public int Add(int a, int b) { return a + b; } }\n"},
		{"php", "util.php", "<?php\nfunction add($a, $b) { return $a + $b; }\n"},
		{"rust", "util.rs", "pub fn add(a: i32, b: i32) -> i32 { a + b }\n"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			edges := detectORM(t, c.lang, c.path, c.src)
			if len(edges) != 0 {
				t.Errorf("expected zero QUERIES edges on non-ORM %s file, got %d: %+v", c.lang, len(edges), edges)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CRUD verb coverage: a single Prisma fixture exercises all four canonical
// operations so the canonicalOp() table doesn't silently regress.
// ---------------------------------------------------------------------------

func TestORM_AllCRUDVerbs(t *testing.T) {
	src := `import { prisma } from './client'
export async function a() { return prisma.user.findUnique({ where: { id: 1 } }) }
export async function b() { return prisma.user.create({ data: {} }) }
export async function c() { return prisma.user.update({ where: { id: 1 }, data: {} }) }
export async function d() { return prisma.user.delete({ where: { id: 1 } }) }
export async function e() { return prisma.user.count() }
`
	edges := detectORM(t, "typescript", "src/all_verbs.ts", src)
	wantOps := map[string]string{
		"a": "find",
		"b": "create",
		"c": "update",
		"d": "delete",
		"e": "aggregate",
	}
	got := map[string]string{}
	for _, e := range edges {
		// Strip "Function:" prefix.
		fn := strings.TrimPrefix(e.From, "Function:")
		got[fn] = e.Op
	}
	for fn, op := range wantOps {
		if got[fn] != op {
			t.Errorf("function %s: want op=%q, got %q (all edges: %+v)", fn, op, got[fn], edges)
		}
	}
}
