<?php
// Synthetic fixture based on common Laravel facade usage patterns.
// Covers: Route, Auth, DB, Cache, Log, Storage, Mail, Queue, Schema,
// Validator, Hash facades — all resolved via the IoC container at runtime
// and therefore statically unresolvable by the PHP extractor.
// License: synthetic, no upstream source.

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Mail;
use Illuminate\Support\Facades\Queue;
use Illuminate\Support\Facades\Redirect;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\Schema;
use Illuminate\Support\Facades\Session;
use Illuminate\Support\Facades\Storage;
use Illuminate\Support\Facades\Validator;

class PostController extends Controller
{
    public function index(Request $request)
    {
        // Auth facade — IoC-resolved, runtime dispatch
        $user = Auth::user();
        $id   = Auth::id();
        if (Auth::check()) {
            Log::info('Authenticated user', ['id' => $id]);
        }

        // Cache facade — runtime-bound to cache driver
        $posts = Cache::remember('posts.all', 3600, function () {
            return DB::table('posts')->get();
        });

        // DB facade — resolves to current connection at runtime
        $count  = DB::table('posts')->where('status', 'published')->count();
        $recent = DB::select('SELECT * FROM posts ORDER BY created_at DESC LIMIT 5');

        return response()->json(['posts' => $posts, 'count' => $count]);
    }

    public function store(Request $request)
    {
        // Validator facade — runtime-dispatched validation
        $validated = Validator::make($request->all(), [
            'title'   => 'required|string|max:255',
            'content' => 'required|string',
        ])->validate();

        // Hash facade — runtime-bound to hashing driver
        $password = Hash::make($request->password);

        // DB transaction via facade
        DB::transaction(function () use ($validated, $password) {
            $post = DB::table('posts')->insertGetId($validated);
            Cache::forget('posts.all');
            Log::channel('posts')->info('Post created', ['id' => $post]);
        });

        // Queue facade — dispatches to runtime-bound queue connection
        Queue::push('App\Jobs\SendWelcomeEmail', ['user_id' => Auth::id()]);

        // Mail facade — runtime-bound to mailer
        Mail::to(Auth::user())->send(new \App\Mail\PostCreated($validated));

        // Storage facade — runtime-bound to filesystem driver
        if ($request->hasFile('cover')) {
            $path = Storage::disk('public')->put('covers', $request->file('cover'));
        }

        return Redirect::route('posts.index');
    }

    public function destroy(int $id)
    {
        // DB::delete shorthand
        DB::delete('DELETE FROM posts WHERE id = ?', [$id]);
        Cache::tags(['posts'])->flush();

        // Log facade
        Log::warning('Post deleted', ['id' => $id, 'by' => Auth::id()]);

        return response()->noContent();
    }
}

// Route definitions file — typical web.php / api.php pattern
// Route facade usage: Route::get, Route::post, Route::put, Route::delete,
// Route::group, Route::prefix, Route::middleware, Route::resource, Route::apiResource
class RouteServiceProvider
{
    public function boot()
    {
        Route::middleware('api')
            ->prefix('api')
            ->group(function () {
                Route::get('/posts', [PostController::class, 'index']);
                Route::post('/posts', [PostController::class, 'store']);
                Route::put('/posts/{id}', [PostController::class, 'update']);
                Route::delete('/posts/{id}', [PostController::class, 'destroy']);
                Route::apiResource('comments', 'CommentController');
            });

        Route::middleware('web')
            ->group(function () {
                Route::resource('pages', 'PageController');
            });
    }
}

class UserController extends Controller
{
    public function login(Request $request)
    {
        $credentials = $request->only('email', 'password');

        if (Auth::attempt($credentials)) {
            Session::regenerate();
            return Redirect::intended('dashboard');
        }

        return back()->withErrors(['email' => 'Invalid credentials']);
    }

    public function logout(Request $request)
    {
        Auth::logout();
        Session::invalidate();
        Session::regenerateToken();
        return Redirect::to('/');
    }
}

class SchemaManager
{
    public function createUsersTable()
    {
        // Schema facade — Blueprint DSL is runtime-dispatched
        Schema::create('users', function ($table) {
            $table->id();
            $table->string('name');
            $table->string('email')->unique();
            $table->string('password');
            $table->rememberToken();
            $table->timestamps();
        });
    }

    public function dropIfExists(string $name)
    {
        Schema::dropIfExists($name);
    }

    public function hasTable(string $name): bool
    {
        return Schema::hasTable($name);
    }
}
