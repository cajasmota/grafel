// AdonisJS auth corpus file (#2852 real-data verification).
import Route from '@ioc:Adonis/Core/Route'

Route.get('/dashboard', 'DashboardController.index').middleware('auth')
Route.get('/about', 'PagesController.about')
