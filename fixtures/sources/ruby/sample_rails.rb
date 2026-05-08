# Sample Rails-like Ruby file — golden fixture source.

class UsersController
  before_action :authenticate_user!
  before_action :set_user, only: [:show, :update, :destroy]

  def index
    @users = User.all
    render json: @users
  end

  def show
    render json: @user
  end

  def create
    @user = User.new(user_params)
    if @user.save
      render json: @user, status: :created
    else
      render json: @user.errors, status: :unprocessable_entity
    end
  end

  def update
    if @user.update(user_params)
      render json: @user
    else
      render json: @user.errors, status: :unprocessable_entity
    end
  end

  def destroy
    @user.destroy
    head :no_content
  end

  private

  def set_user
    @user = User.find(params[:id])
  end

  def user_params
    params.require(:user).permit(:name, :email)
  end

  def authenticate_user!
    raise "Unauthorized" unless current_user
  end
end

class User
  has_many :posts, dependent: :destroy
  has_one :profile, dependent: :destroy
  belongs_to :organization, optional: true

  validates :name, presence: true
  validates :email, presence: true, uniqueness: true

  def full_name
    "#{name}"
  end
end
