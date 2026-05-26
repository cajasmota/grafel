# Source: https://github.com/phoenixframework/phoenix (synthetic based on real Ecto schema patterns) | License: MIT

defmodule MyApp.Blog.Post do
  use Ecto.Schema
  import Ecto.Changeset

  @type t :: %__MODULE__{}

  @statuses [:draft, :published, :archived]

  schema "posts" do
    field :title, :string
    field :slug, :string
    field :excerpt, :string
    field :body, :string
    field :cover_image, :string
    field :status, Ecto.Enum, values: @statuses, default: :draft
    field :published_at, :utc_datetime
    field :views_count, :integer, default: 0

    belongs_to :author, MyApp.Accounts.User
    belongs_to :category, MyApp.Blog.Category

    many_to_many :tags, MyApp.Blog.Tag,
      join_through: "post_tags",
      on_replace: :delete

    has_many :comments, MyApp.Blog.Comment,
      where: [approved: true]

    timestamps(type: :utc_datetime)
  end

  @required_fields ~w(title body)a
  @optional_fields ~w(excerpt cover_image status published_at category_id)a

  @doc false
  def changeset(post, attrs) do
    post
    |> cast(attrs, @required_fields ++ @optional_fields)
    |> validate_required(@required_fields)
    |> validate_length(:title, min: 3, max: 255)
    |> validate_length(:excerpt, max: 500)
    |> validate_inclusion(:status, @statuses)
    |> generate_slug()
    |> unique_constraint(:slug)
    |> foreign_key_constraint(:author_id)
    |> foreign_key_constraint(:category_id)
  end

  def publish_changeset(post) do
    post
    |> change(%{status: :published, published_at: DateTime.utc_now() |> DateTime.truncate(:second)})
    |> validate_required([:published_at])
  end

  defp generate_slug(%Ecto.Changeset{valid?: true, changes: %{title: title}} = changeset) do
    slug = title
    |> String.downcase()
    |> String.replace(~r/[^a-z0-9\s-]/, "")
    |> String.replace(~r/\s+/, "-")
    |> String.trim("-")

    put_change(changeset, :slug, slug)
  end

  defp generate_slug(changeset), do: changeset
end

defmodule MyApp.Blog.Comment do
  use Ecto.Schema
  import Ecto.Changeset

  schema "comments" do
    field :body, :string
    field :approved, :boolean, default: false

    belongs_to :post, MyApp.Blog.Post
    belongs_to :author, MyApp.Accounts.User
    belongs_to :parent, MyApp.Blog.Comment

    has_many :replies, MyApp.Blog.Comment,
      foreign_key: :parent_id

    timestamps(type: :utc_datetime)
  end

  @doc false
  def changeset(comment, attrs) do
    comment
    |> cast(attrs, [:body, :approved, :parent_id])
    |> validate_required([:body])
    |> validate_length(:body, min: 2, max: 5000)
    |> foreign_key_constraint(:post_id)
    |> foreign_key_constraint(:author_id)
    |> foreign_key_constraint(:parent_id)
  end
end

defmodule MyApp.Blog.Tag do
  use Ecto.Schema
  import Ecto.Changeset

  schema "tags" do
    field :name, :string
    field :slug, :string

    many_to_many :posts, MyApp.Blog.Post,
      join_through: "post_tags"

    timestamps(type: :utc_datetime)
  end

  @doc false
  def changeset(tag, attrs) do
    tag
    |> cast(attrs, [:name, :slug])
    |> validate_required([:name, :slug])
    |> validate_length(:name, min: 1, max: 50)
    |> unique_constraint(:slug)
  end
end
