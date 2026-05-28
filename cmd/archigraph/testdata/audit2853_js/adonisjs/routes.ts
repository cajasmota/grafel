// audit2853_js — AdonisJS slice. Named middleware chained onto routes.
import Route from '@ioc:Adonis/Core/Route'

Route.get('/dashboard', 'DashboardController.index').middleware(['auth', 'throttle'])
Route.get('/about', 'PagesController.about')
