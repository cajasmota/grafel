package php_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/php"
	"github.com/cajasmota/grafel/internal/types"
)

func extractPHPFile(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "php",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return got
}

func findPHPEntity(t *testing.T, got []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	for _, e := range got {
		if e.Name == name && e.Kind == "SCOPE.Component" {
			return e
		}
	}
	t.Fatalf("expected SCOPE.Component named %q in %d entities", name, len(got))
	return types.EntityRecord{}
}

func TestEloquent_ModelByShorthandExtends(t *testing.T) {
	src := `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Post extends Model {
    protected $table = 'posts';
}
`
	got := extractPHPFile(t, "app/Models/Post.php", src)
	e := findPHPEntity(t, got, "Post")
	if e.Properties["framework"] != "laravel" {
		t.Errorf("framework=%q, want laravel", e.Properties["framework"])
	}
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model", e.Properties["kind"])
	}
	if e.Properties["orm"] != "eloquent" {
		t.Errorf("orm=%q, want eloquent", e.Properties["orm"])
	}
	if e.Properties["superclass"] != "Model" {
		t.Errorf("superclass=%q, want Model", e.Properties["superclass"])
	}
	if !containsTagPHP(e.Tags, "framework:laravel") {
		t.Errorf("tags=%v, missing framework:laravel", e.Tags)
	}
	if !containsTagPHP(e.Tags, "laravel:model") {
		t.Errorf("tags=%v, missing laravel:model", e.Tags)
	}
}

func TestEloquent_ModelByFullyQualifiedExtends(t *testing.T) {
	src := `<?php
class User extends \Illuminate\Database\Eloquent\Model {
    protected $guarded = [];
}
`
	got := extractPHPFile(t, "User.php", src)
	e := findPHPEntity(t, got, "User")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model (qualified_name superclass)", e.Properties["kind"])
	}
	if e.Properties["orm"] != "eloquent" {
		t.Errorf("orm=%q, want eloquent", e.Properties["orm"])
	}
	if e.Properties["superclass"] != "Illuminate\\Database\\Eloquent\\Model" {
		t.Errorf("superclass=%q, want namespaced Model without leading \\", e.Properties["superclass"])
	}
}

func TestEloquent_ModelByAuthenticatable(t *testing.T) {
	src := `<?php
use Illuminate\Foundation\Auth\User as Authenticatable;

class User extends Authenticatable {
    protected $fillable = ['email'];
}
`
	got := extractPHPFile(t, "app/Models/User.php", src)
	e := findPHPEntity(t, got, "User")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model (Authenticatable superclass)", e.Properties["kind"])
	}
	if e.Properties["orm"] != "eloquent" {
		t.Errorf("orm=%q, want eloquent", e.Properties["orm"])
	}
}

func TestEloquent_ModelByPath(t *testing.T) {
	src := `<?php
class CustomModel extends BaseModel {
    // Project-specific base class, path-only detection must still fire.
}
`
	got := extractPHPFile(t, "app/Models/CustomModel.php", src)
	e := findPHPEntity(t, got, "CustomModel")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model (path-based)", e.Properties["kind"])
	}
	if e.Properties["orm"] != "eloquent" {
		t.Errorf("orm=%q, want eloquent", e.Properties["orm"])
	}
}

func TestEloquent_ControllerByExtends(t *testing.T) {
	src := `<?php
namespace App\Http\Controllers;

class PostController extends Controller {
    public function index() { return []; }
}
`
	got := extractPHPFile(t, "app/Http/Controllers/PostController.php", src)
	e := findPHPEntity(t, got, "PostController")
	if e.Properties["framework"] != "laravel" {
		t.Errorf("framework=%q, want laravel", e.Properties["framework"])
	}
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller", e.Properties["kind"])
	}
	if e.Properties["service_kind"] != "laravel_service" {
		t.Errorf("service_kind=%q, want laravel_service", e.Properties["service_kind"])
	}
	if e.Properties["orm"] != "" {
		t.Errorf("orm=%q, want empty for controller", e.Properties["orm"])
	}
}

func TestEloquent_ControllerByIlluminateRouting(t *testing.T) {
	src := `<?php
class BaseController extends \Illuminate\Routing\Controller {
}
`
	got := extractPHPFile(t, "src/BaseController.php", src)
	e := findPHPEntity(t, got, "BaseController")
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller", e.Properties["kind"])
	}
	if e.Properties["superclass"] != "Illuminate\\Routing\\Controller" {
		t.Errorf("superclass=%q, want namespaced Controller", e.Properties["superclass"])
	}
}

func TestEloquent_ControllerByPath(t *testing.T) {
	src := `<?php
class HealthController {
    public function ping(): string { return "ok"; }
}
`
	got := extractPHPFile(t, "app/Http/Controllers/HealthController.php", src)
	e := findPHPEntity(t, got, "HealthController")
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller (path-based)", e.Properties["kind"])
	}
	if e.Properties["framework"] != "laravel" {
		t.Errorf("framework=%q, want laravel", e.Properties["framework"])
	}
}

func TestEloquent_MigrationByPath(t *testing.T) {
	src := `<?php
use Illuminate\Database\Migrations\Migration;

class CreatePostsTable extends Migration {
    public function up() {}
    public function down() {}
}
`
	got := extractPHPFile(t, "database/migrations/2024_01_01_000000_create_posts_table.php", src)
	e := findPHPEntity(t, got, "CreatePostsTable")
	if e.Properties["framework"] != "laravel" {
		t.Errorf("framework=%q, want laravel", e.Properties["framework"])
	}
	if e.Properties["kind"] != "migration" {
		t.Errorf("kind=%q, want migration", e.Properties["kind"])
	}
	if e.Properties["service_kind"] != "laravel_migration" {
		t.Errorf("service_kind=%q, want laravel_migration", e.Properties["service_kind"])
	}
	if e.Properties["orm"] != "" {
		t.Errorf("orm=%q, want empty for migration", e.Properties["orm"])
	}
}

func TestEloquent_NonLaravelFileUntouched(t *testing.T) {
	src := `<?php
class Calculator {
    public function add(int $a, int $b): int { return $a + $b; }
}
`
	got := extractPHPFile(t, "src/Calculator.php", src)
	e := findPHPEntity(t, got, "Calculator")
	if e.Properties["framework"] != "" {
		t.Errorf("framework=%q, want empty for plain PHP class", e.Properties["framework"])
	}
	if e.Properties["kind"] != "" {
		t.Errorf("kind=%q, want empty for plain PHP class", e.Properties["kind"])
	}
	for _, tag := range e.Tags {
		if tag == "framework:laravel" {
			t.Errorf("plain PHP class should not carry framework:laravel tag, got %v", e.Tags)
		}
	}
}

func TestEloquent_InterfaceUntouched(t *testing.T) {
	// Even inside app/Models/, interfaces must not be tagged as models —
	// they are protocol, not ORM classes.
	src := `<?php
namespace App\Models;

interface Taggable {
    public function tags(): array;
}
`
	got := extractPHPFile(t, "app/Models/Taggable.php", src)
	e := findPHPEntity(t, got, "Taggable")
	if e.Subtype != "interface" {
		t.Fatalf("subtype=%q, want interface", e.Subtype)
	}
	if e.Properties["framework"] != "" {
		t.Errorf("framework=%q, interfaces must not carry framework labels", e.Properties["framework"])
	}
	if e.Properties["orm"] != "" {
		t.Errorf("orm=%q, interfaces must not be tagged with orm", e.Properties["orm"])
	}
}

func TestEloquent_WindowsPathSeparator(t *testing.T) {
	src := `<?php
class Post extends Model {
}
`
	got := extractPHPFile(t, `app\Models\Post.php`, src)
	e := findPHPEntity(t, got, "Post")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model on windows-style path", e.Properties["kind"])
	}
}

func TestEloquent_BaseClauseRaw(t *testing.T) {
	// Regression guard: when the base_clause has no `name` or
	// `qualified_name` child (malformed / unusual grammar), the fallback
	// should still trim the "extends " prefix and root backslash.
	src := `<?php
class Raw extends Model {}
`
	got := extractPHPFile(t, "app/Models/Raw.php", src)
	e := findPHPEntity(t, got, "Raw")
	if e.Properties["superclass"] != "Model" {
		t.Errorf("superclass=%q, want Model", e.Properties["superclass"])
	}
}

func containsTagPHP(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
