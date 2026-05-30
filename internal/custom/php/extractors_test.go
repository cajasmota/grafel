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
// Symfony — deep: routing (method+name)
// ---------------------------------------------------------------------------

// TestSymfonyRouteAttributeWithMethod verifies that #[Route] attributes with
// an explicit methods array emit method-prefixed endpoint names.
func TestSymfonyRouteAttributeWithMethod(t *testing.T) {
	src := `<?php
class ProductController extends AbstractController
{
    #[Route('/products/{id}', methods: ['GET'], name: 'product_show')]
    public function show(int $id): Response {}

    #[Route('/products', methods: ['POST'], name: 'product_create')]
    public function create(Request $request): Response {}
}
`
	ents := extract(t, "custom_php_symfony", fi("ProductController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /products/{id}") {
		t.Error("expected GET /products/{id} endpoint with method prefix")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /products") {
		t.Error("expected POST /products endpoint with method prefix")
	}
}

// TestSymfonyRouteAttributeMultiMethod verifies routes with multiple methods
// emit one entity per method.
func TestSymfonyRouteAttributeMultiMethod(t *testing.T) {
	src := `<?php
class ApiController extends AbstractController
{
    #[Route('/api/items', methods: ['GET', 'HEAD'], name: 'items_list')]
    public function list(): Response {}
}
`
	ents := extract(t, "custom_php_symfony", fi("ApiController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/items") {
		t.Error("expected GET /api/items")
	}
	if !containsEntity(ents, "SCOPE.Operation", "HEAD /api/items") {
		t.Error("expected HEAD /api/items")
	}
}

// TestSymfonyAnnotationRouteWithMethod verifies @Route annotation routes with
// methods and name attributes are parsed correctly.
func TestSymfonyAnnotationRouteWithMethod(t *testing.T) {
	src := `<?php
class LegacyController extends AbstractController
{
    /**
     * @Route("/legacy/users", name="legacy_users", methods={"GET"})
     */
    public function index(): Response {}
}
`
	ents := extract(t, "custom_php_symfony", fi("LegacyController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/legacy/users") {
		t.Error("expected /legacy/users annotation route")
	}
}

// TestSymfonyYAMLRoute verifies that config/routes.yaml route paths are
// extracted with correct method and route name.
func TestSymfonyYAMLRoute(t *testing.T) {
	src := `product_show:
    path: /products/{id}
    controller: App\Controller\ProductController::show
    methods: [GET]

order_create:
    path: /orders
    controller: App\Controller\OrderController::create
    methods: [POST]
`
	ents := extract(t, "custom_php_symfony", fi("config/routes.yaml", "yaml", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /products/{id}") {
		t.Error("expected GET /products/{id} from YAML route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /orders") {
		t.Error("expected POST /orders from YAML route")
	}
}

// ---------------------------------------------------------------------------
// Symfony — deep: auth
// ---------------------------------------------------------------------------

// TestSymfonyIsGrantedAttribute verifies #[IsGranted('ROLE_ADMIN')] is
// extracted with the exact role value.
func TestSymfonyIsGrantedAttribute(t *testing.T) {
	src := `<?php
use Symfony\Component\Security\Http\Attribute\IsGranted;

class AdminController extends AbstractController
{
    #[IsGranted('ROLE_ADMIN')]
    public function dashboard(): Response {}

    #[IsGranted('ROLE_SUPER_ADMIN')]
    public function superPanel(): Response {}
}
`
	ents := extract(t, "custom_php_symfony", fi("AdminController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "isgranted:ROLE_ADMIN") {
		t.Error("expected isgranted:ROLE_ADMIN auth pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "isgranted:ROLE_SUPER_ADMIN") {
		t.Error("expected isgranted:ROLE_SUPER_ADMIN auth pattern")
	}
}

// TestSymfonyDenyAccessUnlessGranted verifies $this->denyAccessUnlessGranted
// calls emit auth patterns with the exact role.
func TestSymfonyDenyAccessUnlessGranted(t *testing.T) {
	src := `<?php
class SecureController extends AbstractController
{
    public function edit(Post $post): Response
    {
        $this->denyAccessUnlessGranted('ROLE_USER');
        $this->denyAccessUnlessGranted('edit', $post);
        return new Response();
    }
}
`
	ents := extract(t, "custom_php_symfony", fi("SecureController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "deny_unless_granted:ROLE_USER") {
		t.Error("expected deny_unless_granted:ROLE_USER auth pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "deny_unless_granted:edit") {
		t.Error("expected deny_unless_granted:edit auth pattern")
	}
}

// TestSymfonyVoterClass verifies that Voter subclasses are extracted.
func TestSymfonyVoterClass(t *testing.T) {
	src := `<?php
use Symfony\Component\Security\Core\Authorization\Voter\Voter;

class PostVoter extends Voter
{
    const EDIT = 'edit';
    const VIEW = 'view';

    protected function supports(string $attribute, mixed $subject): bool
    {
        return in_array($attribute, [self::EDIT, self::VIEW]);
    }

    protected function voteOnAttribute(string $attribute, mixed $subject, TokenInterface $token): bool
    {
        switch ($attribute) {
            case 'edit':
                return $this->canEdit($subject, $token->getUser());
            case 'view':
                return true;
        }
    }
}
`
	ents := extract(t, "custom_php_symfony", fi("PostVoter.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "voter:PostVoter") {
		t.Error("expected voter:PostVoter auth pattern")
	}
}

// ---------------------------------------------------------------------------
// Symfony — deep: validation (Assert constraints)
// ---------------------------------------------------------------------------

// TestSymfonyAssertConstraintsAttribute verifies PHP8 #[Assert\...] constraint
// attributes are extracted with exact constraint names.
func TestSymfonyAssertConstraintsAttribute(t *testing.T) {
	src := `<?php
use Symfony\Component\Validator\Constraints as Assert;

class UserDTO
{
    #[Assert\NotBlank]
    #[Assert\Length(min: 2, max: 100)]
    public string $name = '';

    #[Assert\Email]
    public string $email = '';

    #[Assert\NotBlank]
    #[Assert\Positive]
    public int $age = 0;
}
`
	ents := extract(t, "custom_php_symfony", fi("UserDTO.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "assert:NotBlank") {
		t.Error("expected assert:NotBlank validation pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "assert:Email") {
		t.Error("expected assert:Email validation pattern")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "assert:Positive") {
		t.Error("expected assert:Positive validation pattern")
	}
}

// TestSymfonyAssertConstraintWithArgs verifies that constraints with arguments
// include args in the entity name.
func TestSymfonyAssertConstraintWithArgs(t *testing.T) {
	src := `<?php
use Symfony\Component\Validator\Constraints as Assert;

class ProductInput
{
    #[Assert\Length(min: 2, max: 255)]
    public string $title = '';

    #[Assert\Range(min: 0, max: 9999)]
    public float $price = 0.0;
}
`
	ents := extract(t, "custom_php_symfony", fi("ProductInput.php", "php", src))
	// Entity name includes the args fragment
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && len(e.Name) > 13 && e.Name[:13] == "assert:Length" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected assert:Length(...) constraint with args")
	}
}

// TestSymfonyAssertAnnotationConstraint verifies @Assert\ annotation-style
// constraints are extracted (Symfony 4/5 style).
func TestSymfonyAssertAnnotationConstraint(t *testing.T) {
	src := `<?php
use Symfony\Component\Validator\Constraints as Assert;

class OrderRequest
{
    /**
     * @Assert\NotBlank()
     * @Assert\Length(min=3)
     */
    public $reference;
}
`
	ents := extract(t, "custom_php_symfony", fi("OrderRequest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "assert:NotBlank") {
		t.Error("expected assert:NotBlank from annotation-style constraint")
	}
}

// TestSymfonyValidatorValidateCall verifies $validator->validate($obj)
// programmatic validation calls are detected.
func TestSymfonyValidatorValidateCall(t *testing.T) {
	src := `<?php
use Symfony\Component\Validator\Validator\ValidatorInterface;

class RegistrationService
{
    public function __construct(private ValidatorInterface $validator) {}

    public function register(UserDTO $user): void
    {
        $errors = $this->validator->validate($user);
        if (count($errors) > 0) {
            throw new ValidationException($errors);
        }
    }
}
`
	ents := extract(t, "custom_php_symfony", fi("RegistrationService.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "symfony:validator_validate") {
		t.Error("expected symfony:validator_validate pattern for $validator->validate() call")
	}
}

// ---------------------------------------------------------------------------
// Symfony — deep: middleware (EventSubscriber + kernel listeners)
// ---------------------------------------------------------------------------

// TestSymfonyEventSubscriberWithEvents verifies EventSubscriberInterface
// implementors are extracted along with their subscribed event names.
func TestSymfonyEventSubscriberWithEvents(t *testing.T) {
	src := `<?php
use Symfony\Component\EventDispatcher\EventSubscriberInterface;
use Symfony\Component\HttpKernel\Event\RequestEvent;
use Symfony\Component\HttpKernel\KernelEvents;

class AuthSubscriber implements EventSubscriberInterface
{
    public static function getSubscribedEvents(): array
    {
        return [
            KernelEvents::REQUEST => 'onKernelRequest',
            'kernel.response' => 'onKernelResponse',
        ];
    }

    public function onKernelRequest(RequestEvent $event): void {}
    public function onKernelResponse(): void {}
}
`
	ents := extract(t, "custom_php_symfony", fi("AuthSubscriber.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "AuthSubscriber") {
		t.Error("expected AuthSubscriber event_subscriber pattern")
	}
}

// TestSymfonyKernelEventListener verifies $eventDispatcher->addListener()
// calls emit event patterns.
func TestSymfonyKernelEventListener(t *testing.T) {
	src := `<?php
$eventDispatcher->addListener('kernel.request', [$listener, 'onKernelRequest']);
$eventDispatcher->addListener('kernel.response', [$listener, 'onResponse']);
`
	ents := extract(t, "custom_php_symfony", fi("services.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "event:kernel.request") {
		t.Error("expected event:kernel.request pattern from addListener")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "event:kernel.response") {
		t.Error("expected event:kernel.response pattern from addListener")
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

// ---------------------------------------------------------------------------
// PHP Type System (enum / interface / class / type_alias)
// ---------------------------------------------------------------------------

func TestPHPTypeSystemEnum(t *testing.T) {
	src := `<?php
enum Status: string {
    case Active = 'active';
    case Inactive = 'inactive';
}
enum Color { case Red; case Green; }
`
	ents := extract(t, "custom_php_typesystem", fi("Status.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "Status") {
		t.Error("expected Status enum entity")
	}
	if !containsEntity(ents, "SCOPE.Schema", "Color") {
		t.Error("expected Color enum entity")
	}
}

func TestPHPTypeSystemInterface(t *testing.T) {
	src := `<?php
interface Serializable {
    public function serialize(): string;
}
`
	ents := extract(t, "custom_php_typesystem", fi("Serializable.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "Serializable") {
		t.Error("expected Serializable interface entity")
	}
}

func TestPHPTypeSystemTypeAlias(t *testing.T) {
	src := `<?php
/**
 * @phpstan-type UserId int
 * @psalm-type Status = 'active'|'inactive'
 */
class UserRepo {}
`
	ents := extract(t, "custom_php_typesystem", fi("UserRepo.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "UserId") {
		t.Error("expected UserId type alias entity")
	}
}

// ---------------------------------------------------------------------------
// PHP Observability
// ---------------------------------------------------------------------------

func TestPHPObservabilityLog(t *testing.T) {
	src := `<?php
Log::info('user logged in', ['user_id' => $id]);
error_log('Something went wrong');
`
	ents := extract(t, "custom_php_observability", fi("auth.php", "php", src))
	if !containsEntity(ents, "SCOPE.Config", "php:logging") {
		t.Error("expected php:logging observability entity")
	}
}

func TestPHPObservabilityNoMatch(t *testing.T) {
	src := `<?php $x = 42;`
	ents := extract(t, "custom_php_observability", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// CakePHP
// ---------------------------------------------------------------------------

func TestCakePHPRoute(t *testing.T) {
	src := `<?php
$routes->connect('/articles', ['controller' => 'Articles', 'action' => 'index']);
$routes->get('/users', ['controller' => 'Users', 'action' => 'index']);
$routes->resources('Products');
`
	ents := extract(t, "custom_php_cakephp", fi("routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/articles") {
		t.Error("expected /articles route")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "resource:Products") {
		t.Error("expected resource:Products pattern")
	}
}

func TestCakePHPMiddleware(t *testing.T) {
	src := `<?php
$middlewareQueue->add(new AuthenticationMiddleware($this));
$middlewareQueue->add(new CsrfProtectionMiddleware());
`
	ents := extract(t, "custom_php_cakephp", fi("Application.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "middleware:AuthenticationMiddleware") {
		t.Error("expected AuthenticationMiddleware pattern")
	}
}

// TestCakePHPAuthDollarThis verifies that the CakePHP auth regex correctly
// matches $this->Authentication and $this->Authorization — the two alternations
// that were previously dead due to an unescaped $ acting as end-of-text anchor
// in Go's regexp engine instead of a literal dollar sign.
func TestCakePHPAuthDollarThis(t *testing.T) {
	src := `<?php
$this->Authentication->check();
$this->Authorization->can($user, 'edit');
`
	ents := extract(t, "custom_php_cakephp", fi("AppController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "cakephp:auth") {
		t.Error("expected cakephp:auth entity for $this->Authentication usage (was dead before regex fix)")
	}
}

func TestCakePHPAuthDollarThisAuthorization(t *testing.T) {
	src := `<?php
$this->Authorization->can($user, 'delete');
`
	ents := extract(t, "custom_php_cakephp", fi("PostsController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "cakephp:auth") {
		t.Error("expected cakephp:auth entity for $this->Authorization usage (was dead before regex fix)")
	}
}

// ---------------------------------------------------------------------------
// CodeIgniter
// ---------------------------------------------------------------------------

func TestCodeIgniterRoute(t *testing.T) {
	src := `<?php
$routes->get('/users', 'UserController::index');
$routes->post('/users', 'UserController::create');
$routes->resource('products');
`
	ents := extract(t, "custom_php_codeigniter", fi("Routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/users") {
		t.Error("expected /users route")
	}
}

func TestCodeIgniterFilter(t *testing.T) {
	src := `<?php
class AuthFilter implements FilterInterface {
    public function before(RequestInterface $request, $arguments = null) {}
}
`
	ents := extract(t, "custom_php_codeigniter", fi("AuthFilter.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "filter:AuthFilter") {
		t.Error("expected filter:AuthFilter pattern")
	}
}

// ---------------------------------------------------------------------------
// Lumen
// ---------------------------------------------------------------------------

func TestLumenRoute(t *testing.T) {
	src := `<?php
$app->get('/users', 'UserController@index');
$router->post('/auth/login', 'AuthController@login');
`
	ents := extract(t, "custom_php_lumen", fi("web.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/users") {
		t.Error("expected /users route")
	}
}

// ---------------------------------------------------------------------------
// Slim
// ---------------------------------------------------------------------------

func TestSlimRoute(t *testing.T) {
	src := `<?php
$app->get('/hello/{name}', function (Request $request, Response $response, $args) {
    return $response;
});
$app->post('/api/users', UserController::class . ':create');
`
	ents := extract(t, "custom_php_slim", fi("routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/hello/{name}") {
		t.Error("expected /hello/{name} route")
	}
}

// ---------------------------------------------------------------------------
// WordPress
// ---------------------------------------------------------------------------

func TestWordPressRestRoute(t *testing.T) {
	src := `<?php
register_rest_route('myplugin/v1', '/author/(?P<id>\d+)', [
    'methods' => 'GET',
    'callback' => 'my_awesome_func',
]);
`
	ents := extract(t, "custom_php_wordpress", fi("rest-api.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/myplugin/v1/author/(?P<id>\\d+)") {
		// WordPress REST route: namespace + path joined
		found := false
		for _, e := range ents {
			if e.Kind == "SCOPE.Operation" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected a SCOPE.Operation endpoint from register_rest_route")
		}
	}
}

func TestWordPressAction(t *testing.T) {
	src := `<?php
add_action('init', 'my_init_function');
add_action('wp_enqueue_scripts', 'enqueue_styles');
`
	ents := extract(t, "custom_php_wordpress", fi("plugin.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "action:init") {
		t.Error("expected action:init hook pattern")
	}
}

func TestWordPressCapability(t *testing.T) {
	src := `<?php
if (!current_user_can('manage_options')) { wp_die('Unauthorized'); }
`
	ents := extract(t, "custom_php_wordpress", fi("admin.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "capability:manage_options") {
		t.Error("expected capability:manage_options auth pattern")
	}
}

// ---------------------------------------------------------------------------
// Yii
// ---------------------------------------------------------------------------

func TestYiiController(t *testing.T) {
	src := `<?php
class UserController extends Controller {
    public function actionIndex() {}
}
`
	ents := extract(t, "custom_php_yii", fi("UserController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "UserController") {
		t.Error("expected UserController component")
	}
}

func TestYiiFilter(t *testing.T) {
	src := `<?php
class AuthFilter extends ActionFilter {
    public function beforeAction($action) { return parent::beforeAction($action); }
}
`
	ents := extract(t, "custom_php_yii", fi("AuthFilter.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "filter:AuthFilter") {
		t.Error("expected filter:AuthFilter pattern")
	}
}

// ---------------------------------------------------------------------------
// Phalcon
// ---------------------------------------------------------------------------

func TestPhalconRoute(t *testing.T) {
	src := `<?php
$app->get('/api/robots', function() use ($app) {
    return 'hello';
});
$app->post('/api/robots/add', RobotsController::class);
`
	ents := extract(t, "custom_php_phalcon", fi("routes.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/api/robots") {
		t.Error("expected /api/robots route")
	}
}

// TestPhalconAuthDollarThis verifies that the Phalcon auth regex correctly
// matches $this->auth — previously dead due to an unescaped $ in an alternation
// inside a \b-bounded group ($ is not a word character so the boundary never matched).
func TestPhalconAuthDollarThis(t *testing.T) {
	src := `<?php
if (!$this->auth->isLoggedIn()) {
    return $this->response->redirect('/login');
}
`
	ents := extract(t, "custom_php_phalcon", fi("ControllerBase.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "phalcon:auth") {
		t.Error("expected phalcon:auth entity for $this->auth usage (was dead before regex fix)")
	}
}

// ---------------------------------------------------------------------------
// Laminas
// ---------------------------------------------------------------------------

func TestLaminasRoute(t *testing.T) {
	src := `<?php
return [
    'routes' => [
        'home' => [
            'type' => Literal::class,
            'options' => [
                'route' => '/home',
            ],
        ],
    ],
];
`
	ents := extract(t, "custom_php_laminas", fi("module.config.php", "php", src))
	if !containsEntity(ents, "SCOPE.Operation", "/home") {
		t.Error("expected /home route")
	}
}

func TestLaminasMiddleware(t *testing.T) {
	src := `<?php
class AuthMiddleware implements MiddlewareInterface {
    public function process(ServerRequestInterface $request, RequestHandlerInterface $handler): ResponseInterface {}
}
`
	ents := extract(t, "custom_php_laminas", fi("AuthMiddleware.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "middleware:AuthMiddleware") {
		t.Error("expected middleware:AuthMiddleware pattern")
	}
}

// ---------------------------------------------------------------------------
// Yii auth dollar-this regression
// ---------------------------------------------------------------------------

// TestYiiAuthDollarApp verifies that the Yii auth regex correctly matches
// Yii::$app->user — previously dead due to unescaped $ inside an alternation
// group with a leading \b word-boundary ($ is not a word character).
func TestYiiAuthDollarApp(t *testing.T) {
	src := `<?php
$identity = Yii::$app->user->identity;
if (Yii::$app->user->isGuest) {
    return $this->redirect(['site/login']);
}
`
	ents := extract(t, "custom_php_yii", fi("SiteController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "yii:auth") {
		t.Error("expected yii:auth entity for Yii::$app->user usage (was dead before regex fix)")
	}
}
