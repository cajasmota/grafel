# Source: https://github.com/phoenixframework/phoenix (synthetic based on real Phoenix context patterns) | License: MIT

defmodule MyApp.Blog do
  @moduledoc """
  The Blog context.
  """

  import Ecto.Query, warn: false
  alias MyApp.Repo
  alias MyApp.Blog.{Post, Comment, Tag}
  alias MyApp.Accounts.User

  @doc """
  Returns the list of posts.

  ## Examples

      iex> list_posts()
      [%Post{}, ...]

  """
  def list_posts(opts \\ []) do
    page = Keyword.get(opts, :page, 1)
    per_page = Keyword.get(opts, :per_page, 10)
    status = Keyword.get(opts, :status, :published)

    Post
    |> where([p], p.status == ^status)
    |> order_by([p], desc: p.inserted_at)
    |> preload([:author, :tags])
    |> Repo.paginate(page: page, page_size: per_page)
  end

  @doc """
  Gets a single post by slug.

  Raises `Ecto.NoResultsError` if the Post does not exist.
  """
  def get_post_by_slug!(slug) do
    Post
    |> where([p], p.slug == ^slug and p.status == :published)
    |> preload([:author, :tags, comments: [:author]])
    |> Repo.one!()
  end

  @doc """
  Creates a post.
  """
  def create_post(%User{} = author, attrs \\ %{}) do
    %Post{}
    |> Post.changeset(attrs)
    |> Ecto.Changeset.put_assoc(:author, author)
    |> Repo.insert()
  end

  @doc """
  Updates a post.
  """
  def update_post(%Post{} = post, attrs) do
    post
    |> Post.changeset(attrs)
    |> Repo.update()
  end

  @doc """
  Deletes a post.
  """
  def delete_post(%Post{} = post) do
    Repo.delete(post)
  end

  @doc """
  Returns an `%Ecto.Changeset{}` for tracking post changes.
  """
  def change_post(%Post{} = post, attrs \\ %{}) do
    Post.changeset(post, attrs)
  end

  @doc """
  Publishes a post by setting status and published_at.
  """
  def publish_post(%Post{} = post) do
    post
    |> Ecto.Changeset.change(%{
      status: :published,
      published_at: DateTime.utc_now()
    })
    |> Repo.update()
  end

  @doc """
  Increments view count for a post.
  """
  def increment_views(%Post{id: id}) do
    Post
    |> where([p], p.id == ^id)
    |> Repo.update_all(inc: [views_count: 1])
  end

  @doc """
  Returns paginated comments for a post.
  """
  def list_comments(%Post{id: post_id}, opts \\ []) do
    page = Keyword.get(opts, :page, 1)

    Comment
    |> where([c], c.post_id == ^post_id and c.approved == true)
    |> order_by([c], asc: c.inserted_at)
    |> preload(:author)
    |> Repo.paginate(page: page, page_size: 20)
  end

  @doc """
  Creates a comment on a post.
  """
  def create_comment(%Post{} = post, %User{} = author, attrs \\ %{}) do
    %Comment{}
    |> Comment.changeset(attrs)
    |> Ecto.Changeset.put_assoc(:post, post)
    |> Ecto.Changeset.put_assoc(:author, author)
    |> Repo.insert()
  end

  @doc """
  Returns all tags ordered by name.
  """
  def list_tags do
    Tag
    |> order_by([t], asc: t.name)
    |> Repo.all()
  end

  @doc """
  Gets or creates tags by name list.
  """
  def get_or_create_tags(names) when is_list(names) do
    names
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == ""))
    |> Enum.map(fn name ->
      slug = Slugger.slugify_downcase(name)
      case Repo.get_by(Tag, slug: slug) do
        nil ->
          {:ok, tag} = Repo.insert(%Tag{name: name, slug: slug})
          tag
        tag ->
          tag
      end
    end)
  end

  @doc """
  Searches posts by query string.
  """
  def search_posts(query_string) when is_binary(query_string) do
    search_term = "%#{query_string}%"

    Post
    |> where([p], p.status == :published)
    |> where([p], ilike(p.title, ^search_term) or ilike(p.body, ^search_term))
    |> order_by([p], desc: p.inserted_at)
    |> limit(20)
    |> preload([:author, :tags])
    |> Repo.all()
  end
end
