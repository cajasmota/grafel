require './app'

use Rack::Session::Cookie, secret: ENV.fetch('SECRET', 'dev')

run MyApp
