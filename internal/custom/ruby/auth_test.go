package ruby_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Auth — Devise
// ---------------------------------------------------------------------------

func TestRubyAuthDeviseFor(t *testing.T) {
	src := `
Rails.application.routes.draw do
  devise_for :users
  devise_for :admins, controllers: { sessions: 'admins/sessions' }
end
`
	ents := extract(t, "ruby_auth", fi("config/routes.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "devise_for:users") {
		t.Error("expected devise_for:users entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "devise_for:admins") {
		t.Error("expected devise_for:admins entity")
	}
}

func TestRubyAuthDeviseAuthenticate(t *testing.T) {
	src := `
class ApplicationController < ActionController::Base
  before_action :authenticate_user!
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/application_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "authenticate_user!") {
		t.Error("expected authenticate_user! entity")
	}
}

func TestRubyAuthDeviseModules(t *testing.T) {
	src := `
class User < ApplicationRecord
  devise :database_authenticatable, :registerable,
         :recoverable, :rememberable, :validatable
end
`
	ents := extract(t, "ruby_auth", fi("app/models/user.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "devise_modules") {
		t.Error("expected devise_modules entity")
	}
}

func TestRubyAuthRequireLogin(t *testing.T) {
	src := `
class PostsController < ApplicationController
  before_filter :require_login
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/posts_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "require_login") {
		t.Error("expected require_login entity")
	}
}

// ---------------------------------------------------------------------------
// Auth — JWT
// ---------------------------------------------------------------------------

func TestRubyAuthJWTEncodeDecode(t *testing.T) {
	src := `
require 'jwt'

def encode_token(payload)
  JWT.encode(payload, Rails.application.secret_key_base)
end

def decode_token(token)
  JWT.decode(token, Rails.application.secret_key_base, true, algorithm: 'HS256')
end
`
	ents := extract(t, "ruby_auth", fi("app/lib/jwt_helper.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "JWT.encode") {
		t.Error("expected JWT.encode entity")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "JWT.decode") {
		t.Error("expected JWT.decode entity")
	}
}

func TestRubyAuthJWTRequireOnly(t *testing.T) {
	src := `require 'jwt'`
	ents := extract(t, "ruby_auth", fi("lib/auth.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "jwt") {
		t.Error("expected jwt require entity")
	}
}

// ---------------------------------------------------------------------------
// Auth — Warden
// ---------------------------------------------------------------------------

func TestRubyAuthWarden(t *testing.T) {
	src := `
use Warden::Manager do |manager|
  manager.default_strategies :password
  manager.failure_app = Proc.new { |env|
    ['401', {'Content-Type' => 'application/json'}, ['']]
  }
end
`
	ents := extract(t, "ruby_auth", fi("config.ru", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Warden::Manager") {
		t.Error("expected Warden::Manager entity")
	}
}

func TestRubyAuthWardenEnv(t *testing.T) {
	src := `
def current_user
  @current_user ||= env['warden'].user
end
`
	ents := extract(t, "ruby_auth", fi("lib/auth_helper.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "env.warden") {
		t.Error("expected env.warden entity")
	}
}

// ---------------------------------------------------------------------------
// Auth — CanCanCan
// ---------------------------------------------------------------------------

func TestRubyAuthCanCanAuthorize(t *testing.T) {
	src := `
class ArticlesController < ApplicationController
  def show
    @article = Article.find(params[:id])
    authorize! :read, @article
  end
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/articles_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "authorize!") {
		t.Error("expected authorize! entity")
	}
}

func TestRubyAuthCanCanLoadAndAuthorize(t *testing.T) {
	src := `
class PostsController < ApplicationController
  load_and_authorize_resource
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/posts_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "load_and_authorize_resource") {
		t.Error("expected load_and_authorize_resource entity")
	}
}

func TestRubyAuthCanCanAbility(t *testing.T) {
	src := `
class Ability
  include CanCan::Ability
  def initialize(user)
    can :manage, :all if user.admin?
    can :read, Article
    cannot :destroy, Article, archived: true
  end
end
`
	ents := extract(t, "ruby_auth", fi("app/models/ability.rb", "ruby", src))
	if len(ents) == 0 {
		t.Error("expected CanCanCan ability entities")
	}
}

// ---------------------------------------------------------------------------
// Auth — Pundit
// ---------------------------------------------------------------------------

func TestRubyAuthPunditAuthorize(t *testing.T) {
	src := `
class ArticlesController < ApplicationController
  include Pundit

  def update
    @article = Article.find(params[:id])
    authorize @article
    @article.update(article_params)
  end

  def index
    @articles = policy_scope(Article)
  end
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/articles_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "authorize") {
		t.Error("expected Pundit authorize entity")
	}
}

func TestRubyAuthPunditPolicy(t *testing.T) {
	src := `
class ArticlePolicy < ApplicationPolicy
  include Pundit

  def update?
    user.admin? || record.author == user
  end
end
`
	ents := extract(t, "ruby_auth", fi("app/policies/article_policy.rb", "ruby", src))
	if len(ents) == 0 {
		t.Error("expected at least one Pundit entity")
	}
}

// ---------------------------------------------------------------------------
// Auth — Doorkeeper
// ---------------------------------------------------------------------------

func TestRubyAuthDoorkeeper(t *testing.T) {
	src := `
class ApiController < ApplicationController
  before_action :doorkeeper_authorize!

  def index
    @resources = Resource.all
  end
end
`
	ents := extract(t, "ruby_auth", fi("app/controllers/api_controller.rb", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "doorkeeper_authorize!") {
		t.Error("expected doorkeeper_authorize! entity")
	}
}

// ---------------------------------------------------------------------------
// Auth — Rack::Auth + OmniAuth
// ---------------------------------------------------------------------------

func TestRubyAuthRackAuth(t *testing.T) {
	src := `
use Rack::Auth::Basic, "Protected Area" do |username, password|
  ActiveSupport::SecurityUtils.secure_compare(
    ::Digest::SHA256.hexdigest(password),
    ::Digest::SHA256.hexdigest(ENV['ADMIN_PASSWORD'])
  )
end
`
	ents := extract(t, "ruby_auth", fi("config.ru", "ruby", src))
	if !containsEntity(ents, "SCOPE.Pattern", "Rack::Auth::Basic") {
		t.Error("expected Rack::Auth::Basic entity")
	}
}

func TestRubyAuthOmniAuth(t *testing.T) {
	src := `
Rails.application.config.middleware.use OmniAuth::Builder do
  provider :github, ENV['GITHUB_KEY'], ENV['GITHUB_SECRET']
  provider :google_oauth2, ENV['GOOGLE_ID'], ENV['GOOGLE_SECRET']
end
`
	ents := extract(t, "ruby_auth", fi("config/initializers/omniauth.rb", "ruby", src))
	if len(ents) == 0 {
		t.Error("expected OmniAuth entities")
	}
}

// ---------------------------------------------------------------------------
// No match: plain Ruby file
// ---------------------------------------------------------------------------

func TestRubyAuthNoMatch(t *testing.T) {
	src := `
class Calculator
  def add(a, b)
    a + b
  end
end
`
	ents := extract(t, "ruby_auth", fi("lib/calculator.rb", "ruby", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
