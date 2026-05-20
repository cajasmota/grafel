defmodule Demo.UserRepo do
  @moduledoc """
  Repository context for User entities. Wraps Ecto.Repo operations
  so controllers don't call Repo directly.
  """

  alias Demo.Repo
  alias Demo.Schemas.User

  def list_users do
    Repo.all(User)
  end

  def get_user!(id) do
    Repo.get!(User, id)
  end

  def get_user(id) do
    Repo.get(User, id)
  end

  def create_user(attrs) do
    %User{}
    |> User.changeset(attrs)
    |> Repo.insert()
  end

  def update_user(%User{} = user, attrs) do
    user
    |> User.changeset(attrs)
    |> Repo.update()
  end

  def delete_user(%User{} = user) do
    Repo.delete(user)
  end

  defp log_action(action, result) do
    require Logger
    Logger.info("UserRepo action=#{action} result=#{inspect(result)}")
    result
  end
end
