package php_test

// laravel_authval_test.go — value-asserting tests for the three deep Laravel
// extractors: custom_php_laravel_auth, custom_php_laravel_validation,
// custom_php_laravel_middleware.

import "testing"

// ============================================================================
// Auth — route-level middleware
// ============================================================================

func TestLaravelAuthMiddlewareSession(t *testing.T) {
	src := `<?php
Route::get('/dashboard', [DashboardController::class, 'index'])->middleware('auth');
`
	ents := extract(t, "custom_php_laravel_auth", fi("web.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "auth:middleware:session") {
		t.Error("expected auth:middleware:session from ->middleware('auth')")
	}
}

func TestLaravelAuthMiddlewareSanctum(t *testing.T) {
	src := `<?php
Route::get('/api/user', [UserController::class, 'me'])->middleware('auth:sanctum');
`
	ents := extract(t, "custom_php_laravel_auth", fi("api.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "auth:middleware:sanctum") {
		t.Error("expected auth:middleware:sanctum guard")
	}
}

func TestLaravelAuthMiddlewareAPI(t *testing.T) {
	src := `<?php
Route::middleware('auth:api')->group(function () {
    Route::get('/profile', [ProfileController::class, 'show']);
});
`
	ents := extract(t, "custom_php_laravel_auth", fi("api.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "auth:middleware:api") {
		t.Error("expected auth:middleware:api guard")
	}
}

func TestLaravelAuthMiddlewareArray(t *testing.T) {
	src := `<?php
Route::get('/orders', [OrderController::class, 'index'])->middleware(['auth:sanctum', 'verified']);
`
	ents := extract(t, "custom_php_laravel_auth", fi("api.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "auth:middleware:sanctum") {
		t.Error("expected auth:middleware:sanctum from array form")
	}
}

// ============================================================================
// Auth — Gate
// ============================================================================

func TestLaravelGateDefine(t *testing.T) {
	src := `<?php
Gate::define('update-post', function (User $user, Post $post) {
    return $user->id === $post->user_id;
});
`
	ents := extract(t, "custom_php_laravel_auth", fi("AuthServiceProvider.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "gate:define:update-post") {
		t.Error("expected gate:define:update-post entity")
	}
}

func TestLaravelGateAuthorize(t *testing.T) {
	src := `<?php
Gate::authorize('admin-access');
Gate::allows('view-analytics', $user);
`
	ents := extract(t, "custom_php_laravel_auth", fi("AdminController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "gate:authorize:admin-access") {
		t.Error("expected gate:authorize:admin-access entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "gate:authorize:view-analytics") {
		t.Error("expected gate:authorize:view-analytics entity")
	}
}

func TestLaravelGatePolicy(t *testing.T) {
	src := `<?php
Gate::policy(Post::class, PostPolicy::class);
`
	ents := extract(t, "custom_php_laravel_auth", fi("AuthServiceProvider.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "gate:policy:Post:PostPolicy") {
		t.Error("expected gate:policy:Post:PostPolicy entity")
	}
}

// ============================================================================
// Auth — controller authorize
// ============================================================================

func TestLaravelControllerAuthorize(t *testing.T) {
	src := `<?php
class PostController extends Controller
{
    public function update(Request $request, Post $post)
    {
        $this->authorize('update', $post);
        $post->update($request->all());
    }
}
`
	ents := extract(t, "custom_php_laravel_auth", fi("PostController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "authorize:update") {
		t.Error("expected authorize:update entity from $this->authorize('update',...)")
	}
}

// ============================================================================
// Auth — Policy class
// ============================================================================

func TestLaravelPolicyClass(t *testing.T) {
	src := `<?php
class PostPolicy
{
    public function viewAny(User $user): bool
    {
        return true;
    }
    public function update(User $user, Post $post): bool
    {
        return $user->id === $post->user_id;
    }
    public function delete(User $user, Post $post): bool
    {
        return $user->isAdmin();
    }
}
`
	ents := extract(t, "custom_php_laravel_auth", fi("PostPolicy.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "policy_class:PostPolicy") {
		t.Error("expected policy_class:PostPolicy component")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "policy:PostPolicy:viewAny") {
		t.Error("expected policy:PostPolicy:viewAny")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "policy:PostPolicy:update") {
		t.Error("expected policy:PostPolicy:update")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "policy:PostPolicy:delete") {
		t.Error("expected policy:PostPolicy:delete")
	}
}

// ============================================================================
// Auth — Blade @can / @cannot / @auth / @guest
// ============================================================================

func TestLaravelBladeCan(t *testing.T) {
	src := `@can('update', $post)
    <a href="{{ route('posts.edit', $post) }}">Edit</a>
@endcan
@cannot('delete', $post)
    <span>No delete</span>
@endcannot`
	ents := extract(t, "custom_php_laravel_auth", fi("show.blade.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "blade:can:update") {
		t.Error("expected blade:can:update entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "blade:cannot:delete") {
		t.Error("expected blade:cannot:delete entity")
	}
}

func TestLaravelBladeAuthSection(t *testing.T) {
	src := `@auth
    <nav>Logged in nav</nav>
@endauth
@guest
    <a href="/login">Login</a>
@endguest`
	ents := extract(t, "custom_php_laravel_auth", fi("layout.blade.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "blade:auth") {
		t.Error("expected blade:auth entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "blade:guest") {
		t.Error("expected blade:guest entity")
	}
}

// ============================================================================
// Auth — auth() helper / Auth:: facade / Sanctum trait
// ============================================================================

func TestLaravelAuthHelper(t *testing.T) {
	src := `<?php
$user = auth()->user();
if (!auth()->check()) {
    return redirect('/login');
}
`
	ents := extract(t, "custom_php_laravel_auth", fi("SomeController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:auth_helper") {
		t.Error("expected laravel:auth_helper entity")
	}
}

func TestLaravelAuthFacade(t *testing.T) {
	src := `<?php
$user = Auth::user();
if (Auth::check()) {
    Auth::logout();
}
`
	ents := extract(t, "custom_php_laravel_auth", fi("AuthController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:auth_facade") {
		t.Error("expected laravel:auth_facade entity")
	}
}

func TestLaravelSanctumTrait(t *testing.T) {
	src := `<?php
use Laravel\Sanctum\HasApiTokens;

class User extends Authenticatable
{
    use HasApiTokens, HasFactory, Notifiable;
}
`
	ents := extract(t, "custom_php_laravel_auth", fi("User.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:sanctum_has_api_tokens") {
		t.Error("expected laravel:sanctum_has_api_tokens entity")
	}
}

func TestLaravelAuthNoMatch(t *testing.T) {
	src := `<?php $x = 42; echo "hello";`
	ents := extract(t, "custom_php_laravel_auth", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no auth entities, got %d", len(ents))
	}
}

// ============================================================================
// Validation — FormRequest
// ============================================================================

func TestLaravelFormRequestClass(t *testing.T) {
	src := `<?php
class StorePostRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }
    public function rules(): array
    {
        return [
            'title'   => 'required|max:255',
            'content' => 'required|min:10',
            'status'  => 'in:draft,published',
        ];
    }
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("StorePostRequest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "StorePostRequest") {
		t.Error("expected StorePostRequest form_request component")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:StorePostRequest:title") {
		t.Error("expected validation_rule:StorePostRequest:title with rules 'required|max:255'")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:StorePostRequest:content") {
		t.Error("expected validation_rule:StorePostRequest:content")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:StorePostRequest:status") {
		t.Error("expected validation_rule:StorePostRequest:status")
	}
	// authorize() in FormRequest → form_request_authorize
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:form_request_authorize") {
		t.Error("expected laravel:form_request_authorize entity")
	}
}

func TestLaravelFormRequestMessages(t *testing.T) {
	src := `<?php
class UpdateUserRequest extends FormRequest
{
    public function rules(): array
    {
        return [
            'email' => 'required|email',
            'name'  => 'required|max:100',
        ];
    }
    public function messages(): array
    {
        return [
            'email.required' => 'Email is required.',
        ];
    }
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("UpdateUserRequest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:UpdateUserRequest:email") {
		t.Error("expected validation_rule:UpdateUserRequest:email")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:form_request_messages") {
		t.Error("expected laravel:form_request_messages entity")
	}
}

// ============================================================================
// Validation — $request->validate
// ============================================================================

func TestLaravelRequestValidateInline(t *testing.T) {
	src := `<?php
class PostController extends Controller
{
    public function store(Request $request)
    {
        $validated = $request->validate([
            'title'   => 'required|string|max:255',
            'body'    => 'required',
            'user_id' => 'required|integer|exists:users,id',
        ]);
    }
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("PostController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:request_validate") {
		t.Error("expected laravel:request_validate pattern")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:title") {
		t.Error("expected validation_rule:title")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:body") {
		t.Error("expected validation_rule:body")
	}
	if !containsEntity(ents, "SCOPE.Schema", "validation_rule:user_id") {
		t.Error("expected validation_rule:user_id")
	}
}

// ============================================================================
// Validation — Validator::make
// ============================================================================

func TestLaravelValidatorMake(t *testing.T) {
	src := `<?php
$validator = Validator::make($request->all(), [
    'title' => 'required|unique:posts|max:255',
    'body'  => 'required',
]);
if ($validator->fails()) {
    return redirect()->back()->withErrors($validator);
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("PostController.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:validator_make") {
		t.Error("expected laravel:validator_make entity")
	}
}

// ============================================================================
// Validation — withValidator / prepareForValidation
// ============================================================================

func TestLaravelWithValidator(t *testing.T) {
	src := `<?php
class StoreOrderRequest extends FormRequest
{
    public function rules(): array { return ['total' => 'required|numeric']; }
    public function withValidator($validator)
    {
        $validator->after(function ($v) {
            if ($this->somethingElseIsInvalid()) {
                $v->errors()->add('field', 'Something is wrong.');
            }
        });
    }
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("StoreOrderRequest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:with_validator") {
		t.Error("expected laravel:with_validator entity")
	}
}

func TestLaravelPrepareForValidation(t *testing.T) {
	src := `<?php
class StoreUserRequest extends FormRequest
{
    public function rules(): array { return ['name' => 'required']; }
    protected function prepareForValidation()
    {
        $this->merge(['slug' => Str::slug($this->slug)]);
    }
}
`
	ents := extract(t, "custom_php_laravel_validation", fi("StoreUserRequest.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:prepare_for_validation") {
		t.Error("expected laravel:prepare_for_validation entity")
	}
}

func TestLaravelValidationNoMatch(t *testing.T) {
	src := `<?php $x = 1; echo "no validation here";`
	ents := extract(t, "custom_php_laravel_validation", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no validation entities, got %d", len(ents))
	}
}

// ============================================================================
// Middleware — Kernel $middleware, $middlewareGroups, $routeMiddleware
// ============================================================================

func TestLaravelKernelGlobalMiddleware(t *testing.T) {
	src := `<?php
namespace App\Http;

use Illuminate\Foundation\Http\Kernel as HttpKernel;

class Kernel extends HttpKernel
{
    protected $middleware = [
        \App\Http\Middleware\TrustProxies::class,
        \Illuminate\Http\Middleware\HandleCors::class,
        \App\Http\Middleware\PreventRequestsDuringMaintenance::class,
        \Illuminate\Foundation\Http\Middleware\ValidatePostSize::class,
    ];
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("Kernel.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:kernel_global_stack") {
		t.Error("expected laravel:kernel_global_stack entity")
	}
	// At least one class stamped
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "middleware" &&
			len(e.Name) > len("kernel_middleware:global:") &&
			e.Name[:len("kernel_middleware:global:")] == "kernel_middleware:global:" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one kernel_middleware:global:X entry")
	}
}

func TestLaravelKernelMiddlewareGroups(t *testing.T) {
	src := `<?php
class Kernel extends HttpKernel
{
    protected $middlewareGroups = [
        'web' => [
            \App\Http\Middleware\EncryptCookies::class,
            \Illuminate\Cookie\Middleware\AddQueuedCookiesToResponse::class,
            \Illuminate\Session\Middleware\StartSession::class,
        ],
        'api' => [
            \Laravel\Sanctum\Http\Middleware\EnsureFrontendRequestsAreStateful::class,
            \Illuminate\Routing\Middleware\ThrottleRequests::class . ':api',
        ],
    ];
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("Kernel.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "laravel:kernel_middleware_groups") {
		t.Error("expected laravel:kernel_middleware_groups entity")
	}
}

func TestLaravelRouteMiddlewareAlias(t *testing.T) {
	src := `<?php
class Kernel extends HttpKernel
{
    protected $routeMiddleware = [
        'auth'             => \App\Http\Middleware\Authenticate::class,
        'auth.basic'       => \Illuminate\Auth\Middleware\AuthenticateWithBasicAuth::class,
        'cache.headers'    => \Illuminate\Http\Middleware\SetCacheHeaders::class,
        'can'              => \Illuminate\Auth\Middleware\Authorize::class,
        'guest'            => \App\Http\Middleware\RedirectIfAuthenticated::class,
        'throttle'         => \Illuminate\Routing\Middleware\ThrottleRequests::class,
        'verified'         => \Illuminate\Auth\Middleware\EnsureEmailIsVerified::class,
    ];
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("Kernel.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "route_middleware:auth") {
		t.Error("expected route_middleware:auth alias entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "route_middleware:throttle") {
		t.Error("expected route_middleware:throttle alias entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "route_middleware:verified") {
		t.Error("expected route_middleware:verified alias entity")
	}
}

func TestLaravelMiddlewareAliases(t *testing.T) {
	// Laravel 10+ uses $middlewareAliases instead of $routeMiddleware
	src := `<?php
class Kernel extends HttpKernel
{
    protected $middlewareAliases = [
        'auth'      => \App\Http\Middleware\Authenticate::class,
        'signed'    => \Illuminate\Routing\Middleware\ValidateSignature::class,
    ];
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("Kernel.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "route_middleware:auth") {
		t.Error("expected route_middleware:auth from $middlewareAliases")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "route_middleware:signed") {
		t.Error("expected route_middleware:signed from $middlewareAliases")
	}
}

// ============================================================================
// Middleware — Custom middleware class with handle(Closure $next)
// ============================================================================

func TestLaravelCustomMiddlewareClass(t *testing.T) {
	src := `<?php
namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;

class EnsureTokenIsValid
{
    public function handle(Request $request, Closure $next)
    {
        if ($request->input('token') !== 'my-secret-token') {
            return redirect('home');
        }
        return $next($request);
    }
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("EnsureTokenIsValid.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "middleware_class:EnsureTokenIsValid") {
		t.Error("expected middleware_class:EnsureTokenIsValid component")
	}
}

func TestLaravelCustomMiddlewareWithTerminate(t *testing.T) {
	src := `<?php
class LogAfterResponse
{
    public function handle($request, Closure $next)
    {
        return $next($request);
    }

    public function terminate($request, $response)
    {
        // log after response is sent
    }
}
`
	ents := extract(t, "custom_php_laravel_middleware", fi("LogAfterResponse.php", "php", src))
	if !containsEntity(ents, "SCOPE.Component", "middleware_class:LogAfterResponse") {
		t.Error("expected middleware_class:LogAfterResponse")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "middleware_terminate:LogAfterResponse") {
		t.Error("expected middleware_terminate:LogAfterResponse for terminate() method")
	}
}

// ============================================================================
// Middleware — Route attachment / withoutMiddleware
// ============================================================================

func TestLaravelRouteAttachMiddleware(t *testing.T) {
	src := `<?php
Route::get('/admin', [AdminController::class, 'index'])->middleware('throttle:60,1');
Route::post('/upload', [UploadController::class, 'store'])->middleware('verified');
`
	ents := extract(t, "custom_php_laravel_middleware", fi("web.php", "php", src))
	if !containsEntity(ents, "SCOPE.Pattern", "route_apply_middleware:throttle:60,1") {
		t.Error("expected route_apply_middleware:throttle:60,1")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "route_apply_middleware:verified") {
		t.Error("expected route_apply_middleware:verified")
	}
}

func TestLaravelRouteWithoutMiddleware(t *testing.T) {
	src := `<?php
Route::post('/webhook', [WebhookController::class, 'handle'])
    ->withoutMiddleware(\App\Http\Middleware\VerifyCsrfToken::class);
`
	ents := extract(t, "custom_php_laravel_middleware", fi("web.php", "php", src))
	// withoutMiddleware entity should exist
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && len(e.Name) > len("route_exclude_middleware:") &&
			e.Name[:len("route_exclude_middleware:")] == "route_exclude_middleware:" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected route_exclude_middleware:X entity from withoutMiddleware()")
	}
}

func TestLaravelMiddlewareNoMatch(t *testing.T) {
	src := `<?php $x = 1; echo "no middleware here";`
	ents := extract(t, "custom_php_laravel_middleware", fi("plain.php", "php", src))
	if len(ents) != 0 {
		t.Errorf("expected no middleware entities, got %d", len(ents))
	}
}
