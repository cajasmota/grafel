// AdonisJS middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Adonis chains named middleware onto routes via .middleware([...]).
import Route from '@ioc:Adonis/Core/Route'

Route.get('/dashboard', 'DashboardController.index').middleware(['auth', 'throttle'])
Route.post('/posts', 'PostsController.store').middleware('auth')
Route.get('/about', 'PagesController.about')
