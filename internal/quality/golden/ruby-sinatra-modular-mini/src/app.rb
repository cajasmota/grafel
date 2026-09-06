require 'sinatra/base'
require 'json'

require_relative 'invoice_repository'
require_relative 'auth_helpers'

# Modular-style Sinatra app. NOTHING here is the first construct in the file:
# the requires come first and every DSL call is indented inside the class body.
# That is the whole point of this fixture (#6917).
class MyApp < Sinatra::Base
  configure :production do
    set :show_exceptions, false
  end

  helpers AuthHelpers

  helpers do
    def json_helper(payload)
      content_type :json
      payload.to_json
    end
  end

  before '/admin/*' do
    halt 401 unless authenticate!
  end

  get '/invoices' do
    json_helper(InvoiceRepository.all)
  end

  post '/invoices' do
    status 201
    json_helper(InvoiceRepository.create(params))
  end

  get '/invoices/:id' do
    json_helper(InvoiceRepository.find(params['id']))
  end

  delete '/invoices/:id' do
    InvoiceRepository.delete(params['id'])
    status 204
  end

  after do
    response.headers['X-App'] = 'MyApp'
  end

  error 404 do
    json_helper(error: 'not found')
  end
end
