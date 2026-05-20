defmodule DemoWeb.UserController do
  use DemoWeb, :controller

  alias Demo.UserRepo

  def index(conn, _params) do
    users = UserRepo.list_users()
    render(conn, :index, users: users)
  end

  def show(conn, %{"id" => id}) do
    user = UserRepo.get_user!(id)
    render(conn, :show, user: user)
  end

  def create(conn, %{"user" => user_params}) do
    case UserRepo.create_user(user_params) do
      {:ok, user} ->
        conn
        |> put_flash(:info, "User created successfully.")
        |> redirect(to: ~p"/users/#{user}")

      {:error, changeset} ->
        conn
        |> put_flash(:error, "Could not create user.")
        |> render(:new, changeset: changeset)
    end
  end

  def update(conn, %{"id" => id, "user" => user_params}) do
    user = UserRepo.get_user!(id)
    case UserRepo.update_user(user, user_params) do
      {:ok, user} ->
        conn
        |> put_flash(:info, "User updated successfully.")
        |> redirect(to: ~p"/users/#{user}")

      {:error, changeset} ->
        render(conn, :edit, user: user, changeset: changeset)
    end
  end

  def delete(conn, %{"id" => id}) do
    user = UserRepo.get_user!(id)
    {:ok, _user} = UserRepo.delete_user(user)

    conn
    |> put_flash(:info, "User deleted successfully.")
    |> redirect(to: ~p"/users")
  end
end
