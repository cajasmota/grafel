defmodule MyAppWeb.UserController do
  use Phoenix.Controller

  def index(conn, _params) do
    json(conn, %{users: []})
  end

  def show(conn, %{"id" => id}) do
    json(conn, %{id: id})
  end

  def create(conn, params) do
    json(conn, %{created: params})
  end
end
