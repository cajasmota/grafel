package ruby_test

// validation_test.go — tests for the ruby_validation extractor.
// Part of #3282.

import (
	"testing"
)

func valExtract(t *testing.T, path, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_validation", fi(path, "ruby", src))
}

// ---------------------------------------------------------------------------
// Rails strong params
// ---------------------------------------------------------------------------

func TestValidation_StrongParamsRequire(t *testing.T) {
	src := `
class ArticlesController < ApplicationController
  def article_params
    params.require(:article).permit(:title, :body, :published)
  end
end
`
	ents := valExtract(t, "app/controllers/articles_controller.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", `strong_params:params.require(:article)`) {
		t.Error("expected strong_params require entity")
	}
}

func TestValidation_StrongParamsPermit(t *testing.T) {
	src := `
def user_params
  params.require(:user).permit(:name, :email, :password)
end
`
	ents := valExtract(t, "app/controllers/users_controller.rb", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "dto_field" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one dto_field entity from .permit()")
	}
}

// ---------------------------------------------------------------------------
// ActiveModel validates
// ---------------------------------------------------------------------------

func TestValidation_ActiveModelValidates(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :name, presence: true
  validates :email, presence: true, format: { with: URI::MailTo::EMAIL_REGEXP }
  validates :age, numericality: { greater_than: 0 }
end
`
	ents := valExtract(t, "app/models/user.rb", src)
	wants := []string{"validates:name", "validates:email", "validates:age"}
	for _, w := range wants {
		if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", w) {
			t.Errorf("expected validates entity %q", w)
		}
	}
}

func TestValidation_ActiveModelValidatesClassic(t *testing.T) {
	src := `
class Post < ActiveRecord::Base
  validates_presence_of :title
  validates_uniqueness_of :slug
  validates_length_of :body
end
`
	ents := valExtract(t, "app/models/post.rb", src)
	wants := []string{
		"validates_presence_of:title",
		"validates_uniqueness_of:slug",
		"validates_length_of:body",
	}
	for _, w := range wants {
		if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", w) {
			t.Errorf("expected classic validates entity %q", w)
		}
	}
}

func TestValidation_ValidateCustomMethod(t *testing.T) {
	src := `
class Order < ApplicationRecord
  validate :must_have_items
  validate :not_expired?

  private

  def must_have_items
    errors.add(:base, "must have at least one item") if line_items.empty?
  end
end
`
	ents := valExtract(t, "app/models/order.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "validate_method:must_have_items") {
		t.Error("expected validate_method:must_have_items entity")
	}
}

// ---------------------------------------------------------------------------
// attr_accessor DTO fields
// ---------------------------------------------------------------------------

func TestValidation_AttrAccessorDTO(t *testing.T) {
	src := `
class UserForm
  include ActiveModel::Model

  attr_accessor :name, :email, :password_confirmation

  validates :name, presence: true
end
`
	ents := valExtract(t, "app/forms/user_form.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "attr:name") {
		t.Error("expected attr:name dto_field entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "attr:email") {
		t.Error("expected attr:email dto_field entity")
	}
}

// ---------------------------------------------------------------------------
// dry-validation
// ---------------------------------------------------------------------------

func TestValidation_DryValidationContract(t *testing.T) {
	src := `
class NewUserContract < Dry::Validation::Contract
  params do
    required(:name).filled(:string)
    required(:email).filled(:string)
    optional(:age).maybe(:integer)
  end

  rule(:email) do
    key.failure("must be valid") unless values[:email].match?(/\A[^@]+@[^@]+\z/)
  end
end
`
	ents := valExtract(t, "app/contracts/new_user_contract.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "dry_validation_contract") {
		t.Error("expected dry_validation_contract entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "dry_required:name") {
		t.Error("expected dry_required:name dto_field entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "dry_optional:age") {
		t.Error("expected dry_optional:age dto_field entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "dry_rule:email") {
		t.Error("expected dry_rule:email validation entity")
	}
}

func TestValidation_DryStructAttribute(t *testing.T) {
	src := `
class UserDTO < Dry::Struct
  attribute :name, Types::Strict::String
  attribute :email, Types::Strict::String
  attribute :age, Types::Coercible::Integer.optional
end
`
	ents := valExtract(t, "app/dtos/user_dto.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "dry_struct_attr:name") {
		t.Error("expected dry_struct_attr:name entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "dry_struct_attr:email") {
		t.Error("expected dry_struct_attr:email entity")
	}
}

// ---------------------------------------------------------------------------
// Grape params block
// ---------------------------------------------------------------------------

func TestValidation_GrapeParamsBlock(t *testing.T) {
	src := `
class UsersAPI < Grape::API
  desc 'Create user'
  params do
    requires :name, type: String, desc: 'User name'
    requires :email, type: String, desc: 'Email'
    optional :role, type: String, default: 'user'
  end
  post '/users' do
    User.create!(declared(params))
  end
end
`
	ents := valExtract(t, "app/api/users_api.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "grape_params_block") {
		t.Error("expected grape_params_block entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "grape_requires:name") {
		t.Error("expected grape_requires:name entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "grape_optional:role") {
		t.Error("expected grape_optional:role entity")
	}
}

// ---------------------------------------------------------------------------
// Hanami params
// ---------------------------------------------------------------------------

func TestValidation_HanamiParams(t *testing.T) {
	src := `
module Web::Controllers::Users
  class Create
    include Hanami::Action::Params

    params do
      param :name
      param :email
    end
  end
end
`
	ents := valExtract(t, "apps/web/controllers/users/create.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "hanami_action_params") {
		t.Error("expected hanami_action_params entity")
	}
	if !containsEntitySubtype(ents, "SCOPE.Schema", "dto_field", "hanami_param:name") {
		t.Error("expected hanami_param:name entity")
	}
}

// ---------------------------------------------------------------------------
// Generic param access (Sinatra / Cuba / Roda / Padrino fallback)
// ---------------------------------------------------------------------------

func TestValidation_GenericParamAccess(t *testing.T) {
	src := `
require 'sinatra'

get '/hello' do
  name = params[:name]
  "Hello, #{name}!"
end
`
	ents := valExtract(t, "app.rb", src)
	if !containsEntitySubtype(ents, "SCOPE.Pattern", "request_validation", "param_access:name") {
		t.Error("expected param_access:name entity for Sinatra-style param access")
	}
}

// ---------------------------------------------------------------------------
// Empty / non-matching files
// ---------------------------------------------------------------------------

func TestValidation_EmptyFile(t *testing.T) {
	ents := valExtract(t, "app/models/empty.rb", "")
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(ents))
	}
}

func TestValidation_NoValidationSignal(t *testing.T) {
	src := `def greet(name); "Hello, #{name}"; end`
	ents := valExtract(t, "lib/utils.rb", src)
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for plain Ruby, got %d", len(ents))
	}
}
