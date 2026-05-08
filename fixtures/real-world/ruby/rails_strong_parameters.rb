# Source: https://github.com/rails/rails/blob/main/actionpack/lib/action_controller/metal/strong_parameters.rb | License: MIT
# frozen_string_literal: true

# :markup: markdown

require "active_support/core_ext/hash/indifferent_access"
require "active_support/core_ext/array/wrap"
require "active_support/core_ext/string/filters"
require "active_support/core_ext/object/to_query"
require "active_support/deep_mergeable"
require "action_dispatch/http/upload"
require "rack/test"
require "stringio"
require "yaml"

module ActionController
  # Raised when a required parameter is missing.
  #
  #     params = ActionController::Parameters.new(a: {})
  #     params.fetch(:b)
  #     # => ActionController::ParameterMissing: param is missing or the value is empty or invalid: b
  #     params.require(:a)
  #     # => ActionController::ParameterMissing: param is missing or the value is empty or invalid: a
  #     params.expect(a: [])
  #     # => ActionController::ParameterMissing: param is missing or the value is empty or invalid: a
  class ParameterMissing < KeyError
    attr_reader :param, :keys # :nodoc:

    def initialize(param, keys = nil) # :nodoc:
      @param = param
      @keys  = keys
      super("param is missing or the value is empty or invalid: #{param}")
    end

    if defined?(DidYouMean::Correctable) && defined?(DidYouMean::SpellChecker)
      include DidYouMean::Correctable # :nodoc:

      def corrections # :nodoc:
        @corrections ||= DidYouMean::SpellChecker.new(dictionary: keys).correct(param.to_s)
      end
    end
  end

  # Raised from `expect!` when an expected parameter is missing or is of an
  # incompatible type.
  #
  #     params = ActionController::Parameters.new(a: {})
  #     params.expect!(:a)
  #     # => ActionController::ExpectedParameterMissing: param is missing or the value is empty or invalid: a
  class ExpectedParameterMissing < ParameterMissing
  end

  # Raised when a supplied parameter is not expected and
  # ActionController::Parameters.action_on_unpermitted_parameters is set to
  # `:raise`.
  #
  #     params = ActionController::Parameters.new(a: "123", b: "456")
  #     params.permit(:c)
  #     # => ActionController::UnpermittedParameters: found unpermitted parameters: :a, :b
  class UnpermittedParameters < IndexError
    attr_reader :params # :nodoc:

    def initialize(params) # :nodoc:
      @params = params
      super("found unpermitted parameter#{'s' if params.size > 1 }: #{params.map { |e| ":#{e}" }.join(", ")}")
    end
  end

  # Raised when a Parameters instance is not marked as permitted and an operation
  # to transform it to hash is called.
  #
  #     params = ActionController::Parameters.new(a: "123", b: "456")
  #     params.to_h
  #     # => ActionController::UnfilteredParameters: unable to convert unpermitted parameters to hash
  class UnfilteredParameters < ArgumentError
    def initialize # :nodoc:
      super("unable to convert unpermitted parameters to hash")
    end
  end

  # Raised when initializing Parameters with keys that aren't strings or symbols.
  #
  #     ActionController::Parameters.new(123 => 456)
  #     # => ActionController::InvalidParameterKey: all keys must be Strings or Symbols, got: Integer
  class InvalidParameterKey < ArgumentError
  end

  # # Action Controller Parameters
  #
  # Allows you to choose which attributes should be permitted for mass updating
  # and thus prevent accidentally exposing that which shouldn't be exposed.
  #
  # Provides methods for filtering and requiring params:
  #
  # *   `expect` to safely permit and require parameters in one step.
  # *   `permit` to filter params for mass assignment.
  # *   `require` to require a parameter or raise an error.
  #
  # Examples:
  #
  #     params = ActionController::Parameters.new({
  #       person: {
  #         name: "Francesco",
  #         age:  22,
  #         role: "admin"
  #       }
  #     })
  #
  #     permitted = params.expect(person: [:name, :age])
  #     permitted # => #<ActionController::Parameters {"name"=>"Francesco", "age"=>22} permitted: true>
  #
  #     Person.first.update!(permitted)
  #     # => #<Person id: 1, name: "Francesco", age: 22, role: "user">
  #
  # Parameters provides two options that control the top-level behavior of new
  # instances:
  #
  # *   `permit_all_parameters` - If it's `true`, all the parameters will be
  #     permitted by default. The default is `false`.
  # *   `action_on_unpermitted_parameters` - Controls behavior when parameters
  #     that are not explicitly permitted are found. The default value is `:log`
  #     in test and development environments, `false` otherwise. The values can
  #     be:
  #     *   `false` to take no action.
  #     *   `:log` to emit an `ActiveSupport::Notifications.instrument` event on
  #         the `unpermitted_parameters.action_controller` topic and log at the
  #         DEBUG level.
  #     *   `:raise` to raise an ActionController::UnpermittedParameters
  #         exception.
  #
  # Examples:
  #
  #     params = ActionController::Parameters.new
  #     params.permitted? # => false
  #
  #     ActionController::Parameters.permit_all_parameters = true
  #
  #     params = ActionController::Parameters.new
  #     params.permitted? # => true
  #
  #     params = ActionController::Parameters.new(a: "123", b: "456")
  #     params.permit(:c)
  #     # => #<ActionController::Parameters {} permitted: true>
  #
  #     ActionController::Parameters.action_on_unpermitted_parameters = :raise
  #
  #     params = ActionController::Parameters.new(a: "123", b: "456")
  #     params.permit(:c)
  #     # => ActionController::UnpermittedParameters: found unpermitted keys: a, b
  #
  # Please note that these options *are not thread-safe*. In a multi-threaded
  # environment they should only be set once at boot-time and never mutated at
  # runtime.
  #
  # You can fetch values of `ActionController::Parameters` using either `:key` or
  # `"key"`.
  #
  #     params = ActionController::Parameters.new(key: "value")
  #     params[:key]  # => "value"
  #     params["key"] # => "value"
  class Parameters
    include ActiveSupport::DeepMergeable

    class_attribute :permit_all_parameters, instance_accessor: false, default: false

    class_attribute :action_on_unpermitted_parameters, instance_accessor: false

    ##
    # :method: deep_merge
    #
    # :call-seq:
    #     deep_merge(other_hash, &block)
    #
    # Returns a new `ActionController::Parameters` instance with `self` and
    # `other_hash` merged recursively.
    #
    # Like with `Hash#merge` in the standard library, a block can be provided to
    # merge values.
    #
    #--
    # Implemented by ActiveSupport::DeepMergeable#deep_merge.

    ##
    # :method: deep_merge!
    #
    # :call-seq:
    #     deep_merge!(other_hash, &block)
    #
    # Same as `#deep_merge`, but modifies `self`.
    #
    #--
    # Implemented by ActiveSupport::DeepMergeable#deep_merge!.

    ##
    # :method: as_json
    #
    # :call-seq:
    #     as_json(options=nil)
    #
    # Returns a hash that can be used as the JSON representation for the parameters.

    ##
    # :method: each_key
    #
    # :call-seq:
    #     each_key(&block)
    #
    # Calls block once for each key in the parameters, passing the key. If no block
    # is given, an enumerator is returned instead.

    ##
    # :method: empty?
    #
    # :call-seq:
    #     empty?()
    #
    # Returns true if the parameters have no key/value pairs.

    ##
    # :method: exclude?
    #
    # :call-seq:
    #     exclude?(key)
    #
    # Returns true if the given key is not present in the parameters.

    ##
    # :method: include?
    #
    # :call-seq:
    #     include?(key)
    #
    # Returns true if the given key is present in the parameters.

    ##
    # :method: keys
    #
    # :call-seq:
    #     keys()
    #
    # Returns a new array of the keys of the parameters.

    ##
    # :method: to_s
    #
    # :call-seq:
    #     to_s()
    #
    # Returns the content of the parameters as a string.

    delegate :keys, :empty?, :exclude?, :include?,
      :as_json, :to_s, :each_key, to: :@parameters

    alias_method :has_key?, :include?
    alias_method :key?, :include?
    alias_method :member?, :include?

    # By default, never raise an UnpermittedParameters exception if these params are
    # present. The default includes both 'controller' and 'action' because they are
    # added by Rails and should be of no concern. One way to change these is to
    # specify `always_permitted_parameters` in your config. For instance:
    #
    #     config.action_controller.always_permitted_parameters = %w( controller action format )
    cattr_accessor :always_permitted_parameters, default: %w( controller action )

    class << self
      def nested_attribute?(key, value) # :nodoc:
        /\A-?\d+\z/.match?(key) && (value.is_a?(Hash) || value.is_a?(Parameters))
      end
    end

    # Returns a new `ActionController::Parameters` instance. Also, sets the
    # `permitted` attribute to the default value of
    # `ActionController::Parameters.permit_all_parameters`.
    #
    #     class Person < ActiveRecord::Base
    #     end
    #
    #     params = ActionController::Parameters.new(name: "Francesco")
    #     params.permitted?  # => false
    #     Person.new(params) # => ActiveModel::ForbiddenAttributesError
    #
    #     ActionController::Parameters.permit_all_parameters = true
    #
    #     params = ActionController::Parameters.new(name: "Francesco")
    #     params.permitted?  # => true
    #     Person.new(params) # => #<Person id: nil, name: "Francesco">
    def initialize(parameters = {}, logging_context = {})
      parameters.each_key do |key|
        unless key.is_a?(String) || key.is_a?(Symbol)
          raise InvalidParameterKey, "all keys must be Strings or Symbols, got: #{key.class}"
        end
      end

      @parameters = parameters.with_indifferent_access
      @logging_context = logging_context
      @permitted = self.class.permit_all_parameters
    end

    # Returns true if another `Parameters` object contains the same content and
    # permitted flag.
    def ==(other)
      if other.respond_to?(:permitted?)
        permitted? == other.permitted? && parameters == other.parameters
      else
        super
      end
    end

    def eql?(other)
      self.class == other.class &&
        permitted? == other.permitted? &&
        parameters.eql?(other.parameters)
    end

    def hash
      [self.class, @parameters, @permitted].hash
    end

    def deconstruct_keys(keys)
      slice(*keys).each.with_object({}) { |(key, value), hash| hash.merge!(key.to_sym => value) }
    end

    # Returns a safe ActiveSupport::HashWithIndifferentAccess representation of the
    # parameters with all unpermitted keys removed.
    #
    #     params = ActionController::Parameters.new({
    #       name: "Senjougahara Hitagi",
    #       oddity: "Heavy stone crab"
    #     })
    #     params.to_h
    #     # => ActionController::UnfilteredParameters: unable to convert unpermitted parameters to hash
    #
    #     safe_params = params.permit(:name)
    #     safe_params.to_h # => {"name"=>"Senjougahara Hitagi"}
    def to_h(&block)
      if permitted?
        convert_parameters_to_hashes(@parameters, :to_h, &block)
      else
        raise UnfilteredParameters
      end
    end

    # Returns a safe `Hash` representation of the parameters with all unpermitted
    # keys removed.
    #
    #     params = ActionController::Parameters.new({
    #       name: "Senjougahara Hitagi",
    #       oddity: "Heavy stone crab"
    #     })
    #     params.to_hash
    #     # => ActionController::UnfilteredParameters: unable to convert unpermitted parameters to hash
    #
    #     safe_params = params.permit(:name)
    #     safe_params.to_hash # => {"name"=>"Senjougahara Hitagi"}
    def to_hash
      to_h.to_hash
    end

    # Returns a string representation of the receiver suitable for use as a URL
    # query string:
    #
    #     params = ActionController::Parameters.new({
    #       name: "David",
    #       nationality: "Danish"
    #     })
    #     params.to_query
    #     # => ActionController::UnfilteredParameters: unable to convert unpermitted parameters to hash
    #
    #     safe_params = params.permit(:name, :nationality)
    #     safe_params.to_query
    #     # => "name=David&nationality=Danish"
    #
    # An optional namespace can be passed to enclose key names:
    #
    #     params = ActionController::Parameters.new({
    #       name: "David",
    #       nationality: "Danish"
    #     })
    #     safe_params = params.permit(:name, :nationality)
    #     safe_params.to_query("user")
    #     # => "user%5Bname%5D=David&user%5Bnationality%5D=Danish"
    #
    # The string pairs `"key=value"` that conform the query string are sorted
    # lexicographically in ascending order.
    def to_query(*args)
      to_h.to_query(*args)
    end
    alias_method :to_param, :to_query

    # Returns an unsafe, unfiltered ActiveSupport::HashWithIndifferentAccess
    # representation of the parameters.
    #
    #     params = ActionController::Parameters.new({
    #       name: "Senjougahara Hitagi",
    #       oddity: "Heavy stone crab"
    #     })
    #     params.to_unsafe_h
    #     # => {"name"=>"Senjougahara Hitagi", "oddity" => "Heavy stone crab"}
    def to_unsafe_h
      convert_parameters_to_hashes(@parameters, :to_unsafe_h)
    end
    alias_method :to_unsafe_hash, :to_unsafe_h

    # Convert all hashes in values into parameters, then yield each pair in the same
    # way as `Hash#each_pair`.
    def each_pair(&block)
      return to_enum(__callee__) unless block_given?
      @parameters.each_pair do |key, value|
        yield [key, convert_hashes_to_parameters(key, value)]
      end

      self
    end
    alias_method :each, :each_pair

    # Convert all hashes in values into parameters, then yield each value in the
    # same way as `Hash#each_value`.
    def each_value(&block)
      return to_enum(:each_value) unless block_given?
      @parameters.each_pair do |key, value|
        yield convert_hashes_to_parameters(key, value)
      end

      self
    end

    # Returns a new array of the values of the parameters.
    def values
      to_enum(:each_value).to_a
    end

    # Attribute that keeps track of converted arrays, if any, to avoid double
    # looping in the common use case permit + mass-assignment. Defined in a method
    # to instantiate it only if needed.
    #
    # Testing membership still loops, but it's going to be faster than our own loop
    # that converts values. Also, we are not going to build a new array object per
    # fetch.
    def converted_arrays
      @converted_arrays ||= Set.new
    end

    # Returns `true` if the parameter is permitted, `false` otherwise.
    #
    #     params = ActionController::Parameters.new
    #     params.permitted? # => false
    #     params.permit!
    #     params.permitted? # => true
    def permitted?
      @permitted
    end

    # Sets the `permitted` attribute to `true`. This can be used to pass mass
    # assignment. Returns `self`.
    #
    #     class Person < ActiveRecord::Base
    #     end
    #
    #     params = ActionController::Parameters.new(name: "Francesco")
    #     params.permitted?  # => false
    #     Person.new(params) # => ActiveModel::ForbiddenAttributesError
    #     params.permit!
    #     params.permitted?  # => true
    #     Person.new(params) # => #<Person id: nil, name: "Francesco">
    def permit!
      each_pair do |key, value|
        Array.wrap(value).flatten.each do |v|
          v.permit! if v.respond_to? :permit!
        end
      end

      @permitted = true
      self
    end

    # This method accepts both a single key and an array of keys.
    #
    # When passed a single key, if it exists and its associated value is either
    # present or the singleton `false`, returns said value:
    #
    #     ActionController::Parameters.new(person: { name: "Francesco" }).require(:person)
    #     # => #<ActionController::Parameters {"name"=>"Francesco"} permitted: false>
    #
    # Otherwise raises ActionController::ParameterMissing:
    #
    #     ActionController::Parameters.new.require(:person)
    #     # ActionController::ParameterMissing: param is missing or the value is empty or invalid: person
    #
    #     ActionController::Parameters.new(person: nil).require(:person)
    #     # ActionController::ParameterMissing: param is missing or the value is empty or invalid: person
    #
    #     ActionController::Parameters.new(person: "\t").require(:person)
    #     # ActionController::ParameterMissing: param is missing or the value is empty or invalid: person
    #
    #     ActionController::Parameters.new(person: {}).require(:person)
    #     # ActionController::ParameterMissing: param is missing or the value is empty or invalid: person
    #
    # When given an array of keys, the method tries to require each one of them in
    # order. If it succeeds, an array with the respective return values is returned:
    #
