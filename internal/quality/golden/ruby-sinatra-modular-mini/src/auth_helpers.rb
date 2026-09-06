module AuthHelpers
  def authenticate!
    !env['HTTP_AUTHORIZATION'].nil?
  end
end
