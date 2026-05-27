defmodule MyAppWeb.WidgetController do
  use Phoenix.Controller

  def index(conn, _params) do
    json(conn, %{widgets: []})
  end

  def show(conn, %{"id" => id}) do
    json(conn, %{id: id})
  end

  def create(conn, params) do
    json(conn, %{created: params})
  end
end
