package php_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/php"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Laravel
// ---------------------------------------------------------------------------

func TestLaravelRoute(t *testing.T) {
	src := `
Route::get('/users', [UserController::class, 'index']);
Route::post('/users', [UserController::class, 'store']);
Route::delete('/users/{id}', [UserController::class, 'destroy']);
`
	ents := extract(t, "custom_php_laravel", fi("routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestLaravelResourceRoute(t *testing.T) {
	src := `Route::resource('photos', PhotoController::class);`
	ents := extract(t, "custom_php_laravel", fi("routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /photos") {
		t.Error("expected GET /photos from resource")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /photos") {
		t.Error("expected POST /photos from resource")
	}
}

func TestLaravelBinding(t *testing.T) {
	src := `$this->app->singleton('PaymentGateway', function ($app) { return new StripeGateway(); });`
	ents := extract(t, "custom_php_laravel", fi("AppServiceProvider.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "bind:singleton:PaymentGateway") {
		t.Error("expected singleton binding pattern")
	}
}

func TestLaravelArtisan(t *testing.T) {
	src := `
class SendEmails extends Command
{
    protected $signature = 'mail:send {user}';
}
`
	ents := extract(t, "custom_php_laravel", fi("SendEmails.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "artisan:mail:send {user}") {
		t.Error("expected Artisan command signature")
	}
}

func TestLaravelNoMatch(t *testing.T) {
	src := `<?php echo "hello"; ?>`
	ents := extract(t, "custom_php_laravel", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Symfony
// ---------------------------------------------------------------------------

func TestSymfonyRouteAttribute(t *testing.T) {
	src := `
class UserController extends AbstractController
{
    #[Route('/api/users', methods: ['GET'])]
    public function list(): Response {}

    #[Route('/api/orders', methods: ['POST'])]
    public function create(): Response {}
}
`
	ents := extract(t, "custom_php_symfony", fi("UserController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/api/users") {
		t.Error("expected /api/users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "/api/orders") {
		t.Error("expected /api/orders route")
	}
}

func TestSymfonyDoctrineEntity(t *testing.T) {
	src := `
#[ORM\Entity]
class Product
{
    #[ORM\Id]
    private int $id;
}
`
	ents := extract(t, "custom_php_symfony", fi("Product.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "Product") {
		t.Error("expected Product Doctrine entity schema")
	}
}

func TestSymfonyController(t *testing.T) {
	src := `
class OrderController extends AbstractController {}
`
	ents := extract(t, "custom_php_symfony", fi("OrderController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "OrderController") {
		t.Error("expected OrderController component")
	}
}

func TestSymfonyNoMatch(t *testing.T) {
	src := `<?php $x = 1;`
	ents := extract(t, "custom_php_symfony", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Eloquent
// ---------------------------------------------------------------------------

func TestEloquentModel(t *testing.T) {
	src := `
class Post extends Model
{
    protected $table = 'posts';
}
`
	ents := extract(t, "custom_php_eloquent", fi("Post.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "Post") {
		t.Error("expected Post Eloquent model schema")
	}
}

func TestEloquentRelationship(t *testing.T) {
	src := `
class User extends Model
{
    public function posts(): HasMany
    {
        return $this->hasMany(Post::class);
    }
    public function profile(): HasOne
    {
        return $this->hasOne(Profile::class);
    }
}
`
	ents := extract(t, "custom_php_eloquent", fi("User.php", "php", src))
	// Eloquent relationship entity name = method name
	if !containsEntity(ents, "SCOPE.Component", "posts") {
		t.Error("expected posts relationship component")
	}
	if !containsEntity(ents, "SCOPE.Component", "profile") {
		t.Error("expected profile relationship component")
	}
}

func TestEloquentScope(t *testing.T) {
	src := `
class Post extends Model
{
    public function scopePublished($query)
    {
        return $query->where('published', true);
    }
}
`
	ents := extract(t, "custom_php_eloquent", fi("Post.php", "php", src))
	// Eloquent scope entity name = "scope" + PascalName
	if !containsEntity(ents, "SCOPE.Operation", "scopePublished") {
		t.Error("expected scopePublished operation")
	}
}

func TestEloquentNoMatch(t *testing.T) {
	src := `<?php function helper() {}`
	ents := extract(t, "custom_php_eloquent", fi("helper.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
