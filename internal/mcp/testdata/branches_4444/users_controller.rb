class UsersController < ApplicationController
  # create is a representative branchy Rails action: an env-gate that disables
  # signups outside production, leading validation guards that render error
  # statuses, and a begin/rescue that re-raises after logging. Used verbatim by
  # the #4444 effects-branches Ruby live-validation test.
  def create
    head :service_unavailable unless ENV['SIGNUP_ENABLED'] == 'true'

    if params[:email].blank?
      render json: { error: 'email required' }, status: :bad_request
      return
    end

    return render(status: :conflict) if User.exists?(email: params[:email])

    begin
      user = User.create!(create_params)
      audit_log(user)
    rescue ActiveRecord::RecordInvalid => e
      logger.error("create failed: #{e.message}")
      raise
    end

    render json: user, status: :created
  end

  private

  def create_params
    params.require(:user).permit(:email, :name)
  end
end
