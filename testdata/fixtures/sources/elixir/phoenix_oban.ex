defmodule SampleApi.NotificationController do
  @moduledoc """
  Elixir Phoenix + Oban fixture.
  Demonstrates: HTTP endpoints, Oban job producer, Oban job consumer, DB access via Ecto.
  """
  use SampleApi.Web, :controller

  alias SampleApi.{Notification, Repo, Workers.NotificationWorker}

  def index(conn, _params) do
    notifications = Repo.all(Notification)
    render(conn, "index.json", notifications: notifications)
  end

  def show(conn, %{"id" => id}) do
    notification = Repo.get!(Notification, id)
    render(conn, "show.json", notification: notification)
  end

  def create(conn, %{"notification" => params}) do
    changeset = Notification.changeset(%Notification{}, params)
    case Repo.insert(changeset) do
      {:ok, notification} ->
        # Enqueue Oban job to deliver the notification
        %{notification_id: notification.id}
        |> NotificationWorker.new()
        |> Oban.insert()

        conn
        |> put_status(:created)
        |> render("show.json", notification: notification)

      {:error, changeset} ->
        conn
        |> put_status(:unprocessable_entity)
        |> render("error.json", changeset: changeset)
    end
  end

  def delete(conn, %{"id" => id}) do
    notification = Repo.get!(Notification, id)
    Repo.delete!(notification)
    send_resp(conn, :no_content, "")
  end
end

defmodule SampleApi.Notification do
  use Ecto.Schema
  import Ecto.Changeset

  schema "notifications" do
    field :recipient, :string
    field :message, :string
    field :status, :string, default: "pending"
    timestamps()
  end

  def changeset(notification, attrs) do
    notification
    |> cast(attrs, [:recipient, :message, :status])
    |> validate_required([:recipient, :message])
  end
end

defmodule SampleApi.Workers.NotificationWorker do
  @moduledoc """
  Oban worker that delivers notifications asynchronously.
  """
  use Oban.Worker, queue: :notifications, max_attempts: 3

  alias SampleApi.{Notification, Repo}

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"notification_id" => id}}) do
    notification = Repo.get!(Notification, id)

    case deliver(notification) do
      :ok ->
        notification
        |> Notification.changeset(%{status: "delivered"})
        |> Repo.update()

        :ok

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp deliver(%Notification{recipient: recipient, message: message}) do
    # Simulate delivery
    IO.puts("Sending to #{recipient}: #{message}")
    :ok
  end
end

defmodule SampleApi.Workers.RetryWorker do
  @moduledoc """
  Oban worker that retries failed notifications.
  """
  use Oban.Worker, queue: :retries, max_attempts: 5

  alias SampleApi.{Notification, Repo}

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"notification_id" => id}}) do
    notification = Repo.get!(Notification, id)
    IO.puts("Retrying delivery for #{notification.recipient}")
    :ok
  end
end
