// AdonisJS auth_coverage fixture (#2852). Hand-written.
import Route from '@ioc:Adonis/Core/Route'

// Route-chain .middleware('auth') → high confidence protected.
Route.get('/dashboard', 'DashboardController.index').middleware('auth')
Route.post('/posts', 'PostsController.store').middleware(['auth', 'acl:author'])

// Public route — no auth middleware.
Route.get('/about', 'PagesController.about')
