# Source: https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/controller.ex | License: MIT
defmodule Phoenix.Controller do
  import Plug.Conn
  alias Plug.Conn.AlreadySentError

  require Logger

  @unsent [:unset, :set, :set_chunked, :set_file]

  # View/Layout deprecation plan
  # 1. DONE! Deprecate :namespace option in favor of :layouts on use
  # 2. Deprecate the :layouts option in use Phoenix.Controller
  # 3. Deprecate setting a non-format view/layout on put_*
  # 4. Deprecate rendering a view/layout from :_

  @type view :: atom()
  @type layout :: {module(), layout_name :: atom()} | false

  @moduledoc """
  Controllers are used to group common functionality in the same
  (pluggable) module.

  For example, the route:

      get "/users/:id", MyAppWeb.UserController, :show

  will invoke the `show/2` action in the `MyAppWeb.UserController`:

      defmodule MyAppWeb.UserController do
        use MyAppWeb, :controller

        def show(conn, %{"id" => id}) do
          user = Repo.get(User, id)
          render(conn, :show, user: user)
        end
      end

  An action is a regular function that receives the connection
  and the request parameters as arguments. The connection is a
  `Plug.Conn` struct, as specified by the Plug library.

  Then we invoke `render/3`, passing the connection, the template
  to render (typically named after the action), and the `user: user`
  as assigns. We will explore all of those concepts next.

  ## Connection

  A controller by default provides many convenience functions for
  manipulating the connection, rendering templates, and more.

  Those functions are imported from two modules:

    * `Plug.Conn` - a collection of low-level functions to work with
      the connection

    * `Phoenix.Controller` - functions provided by Phoenix
      to support rendering, and other Phoenix specific behaviour

  If you want to have functions that manipulate the connection
  without fully implementing the controller, you can import both
  modules directly instead of `use Phoenix.Controller`.

  ## Rendering

  One of the main features provided by controllers is the ability
  to perform content negotiation and render templates based on
  information sent by the client.

  There are two ways to render content in a controller. One option
  is to invoke format-specific functions, such as `html/2` and `json/2`.

  However, most commonly controllers invoke custom modules called
  views. Views are modules capable of rendering a custom format.
  This is done by specifying the option `:formats` when defining
  the controller:

      use Phoenix.Controller, formats: [:html, :json]

   Now, when invoking `render/3`, a controller named `MyAppWeb.UserController`
   will invoke `MyAppWeb.UserHTML` and `MyAppWeb.UserJSON` respectively
   when rendering each format:

      def show(conn, %{"id" => id}) do
        user = Repo.get(User, id)
        # Will invoke UserHTML.show(%{user: user}) for html requests
        # Will invoke UserJSON.show(%{user: user}) for json requests
        render(conn, :show, user: user)
      end

  You can also specify formats to render by calling `put_view/2`
  directly with a connection. For example, instead of inferring the
  the view names from the controller, as done in:

      use Phoenix.Controller, formats: [:html, :json]

  You can write the above explicitly in your actions as:

      put_view(conn, html: MyAppWeb.UserHTML, json: MyAppWeb.UserJSON)

  Or as a plug:

      plug :put_view, html: MyAppWeb.UserHTML, json: MyAppWeb.UserJSON

  ## Layouts

  Many applications have shared content that they want to include on every
  page, most often the `<head>` tag and its contents. In Phoenix, this is
  done via the `put_root_layout` function:

      put_root_layout(conn, html: {MyAppWeb.Layouts, :root})

  In most applications, this is invoked as a Plug in your application router:

      plug :put_root_layout, html: {MyAppWeb.Layouts, :root}

  This layout is shared by all controllers, and also by `Phoenix.LiveView`.

  However, you can also specify controller-specific layouts using `put_layout/2`,
  although this functionality is discouraged in Phoenix v1.8 in favor of using
  function components to build your application.

  ## Options

  When used, the controller supports the following options to customize
  template rendering:

    * `:formats` - the formats this controller will render
      by default. For example, specifying `formats: [:html, :json]`
      for a controller named `MyAppWeb.UserController` will
      invoke `MyAppWeb.UserHTML` and `MyAppWeb.UserJSON` when
      respectively rendering each format.

  The `:formats` option is required. You may set it to an empty list
  if you don't expect to render any format upfront. To retain the
  behaviour of older Phoenix versions, you can explicitly pass the
  "View" suffix to the `:formats` option:

      use Phoenix.Controller, formats: [html: "View", json: "View"]

  ## Plug pipeline

  As with routers, controllers also have their own plug pipeline.
  However, different from routers, controllers have a single pipeline:

      defmodule MyAppWeb.UserController do
        use MyAppWeb, :controller

        plug :authenticate, usernames: ["jose", "eric", "sonny"]

        def show(conn, params) do
          # authenticated users only
        end

        defp authenticate(conn, options) do
          if get_session(conn, :username) in options[:usernames] do
            conn
          else
            conn |> redirect(to: "/") |> halt()
          end
        end
      end

  The `:authenticate` plug will be invoked before the action. If the
  plug calls `Plug.Conn.halt/1` (which is by default imported into
  controllers), it will halt the pipeline and won't invoke the action.

  ### Guards

  `plug/2` in controllers supports guards, allowing a developer to configure
  a plug to only run in some particular action.

      plug :do_something when action in [:show, :edit]

  Due to operator precedence in Elixir, if the second argument is a keyword list,
  we need to wrap the keyword in `[...]` when using `when`:

      plug :authenticate, [usernames: ["jose", "eric", "sonny"]] when action in [:show, :edit]
      plug :authenticate, [usernames: ["admin"]] when not action in [:index]

  The first plug will run only when action is show or edit. The second plug will
  always run, except for the index action.

  Those guards work like regular Elixir guards and the only variables accessible
  in the guard are `conn`, the `action` as an atom and the `controller` as an
  alias.

  ## Controllers are plugs

  Like routers, controllers are plugs, but they are wired to dispatch
  to a particular function which is called an action.

  For example, the route:

      get "/users/:id", UserController, :show

  will invoke `UserController` as a plug:

      UserController.call(conn, :show)

  which will trigger the plug pipeline and which will eventually
  invoke the inner action plug that dispatches to the `show/2`
  function in `UserController`.

  As controllers are plugs, they implement both [`init/1`](`c:Plug.init/1`) and
  [`call/2`](`c:Plug.call/2`), and it also provides a function named `action/2`
  which is responsible for dispatching the appropriate action
  after the plug stack (and is also overridable).

  ### Overriding `action/2` for custom arguments

  Phoenix injects an `action/2` plug in your controller which calls the
  function matched from the router. By default, it passes the conn and params.
  In some cases, overriding the `action/2` plug in your controller is a
  useful way to inject arguments into your actions that you would otherwise
  need to repeatedly fetch off the connection. For example, imagine if you
  stored a `conn.assigns.current_user` in the connection and wanted quick
  access to the user for every action in your controller:

      def action(conn, _) do
        args = [conn, conn.params, conn.assigns.current_user]
        apply(__MODULE__, action_name(conn), args)
      end

      def index(conn, _params, user) do
        videos = Repo.all(user_videos(user))
        # ...
      end

      def delete(conn, %{"id" => id}, user) do
        video = Repo.get!(user_videos(user), id)
        # ...
      end

  """
  defmacro __using__(opts) do
    opts =
      if Macro.quoted_literal?(opts) do
        Macro.prewalk(opts, &expand_alias(&1, __CALLER__))
      else
        opts
      end

    quote bind_quoted: [opts: opts] do
      import Phoenix.Controller
      import Plug.Conn

      use Phoenix.Controller.Pipeline

      with {layout, view} <- Phoenix.Controller.__plugs__(__MODULE__, opts) do
        plug :put_new_layout, layout
        plug :put_new_view, view
      end
    end
  end

  defp expand_alias({:__aliases__, _, _} = alias, env),
    do: Macro.expand(alias, %{env | function: {:action, 2}})

  defp expand_alias(other, _env), do: other

  @doc """
  Registers the plug to call as a fallback to the controller action.

  A fallback plug is useful to translate common domain data structures
  into a valid `%Plug.Conn{}` response. If the controller action fails to
  return a `%Plug.Conn{}`, the provided plug will be called and receive
  the controller's `%Plug.Conn{}` as it was before the action was invoked
  along with the value returned from the controller action.

  ## Examples

      defmodule MyController do
        use Phoenix.Controller

        action_fallback MyFallbackController

        def show(conn, %{"id" => id}, current_user) do
          with {:ok, post} <- Blog.fetch_post(id),
               :ok <- Authorizer.authorize(current_user, :view, post) do

            render(conn, "show.json", post: post)
          end
        end
      end

  In the above example, `with` is used to match only a successful
  post fetch, followed by valid authorization for the current user.
  In the event either of those fail to match, `with` will not invoke
  the render block and instead return the unmatched value. In this case,
  imagine `Blog.fetch_post/2` returned `{:error, :not_found}` or
  `Authorizer.authorize/3` returned `{:error, :unauthorized}`. For cases
  where these data structures serve as return values across multiple
  boundaries in our domain, a single fallback module can be used to
  translate the value into a valid response. For example, you could
  write the following fallback controller to handle the above values:

      defmodule MyFallbackController do
        use Phoenix.Controller

        def call(conn, {:error, :not_found}) do
          conn
          |> put_status(:not_found)
          |> put_view(MyErrorView)
          |> render(:"404")
        end

        def call(conn, {:error, :unauthorized}) do
          conn
          |> put_status(:forbidden)
          |> put_view(MyErrorView)
          |> render(:"403")
        end
      end
  """
  defmacro action_fallback(plug) do
    Phoenix.Controller.Pipeline.__action_fallback__(plug, __CALLER__)
  end

  @doc """
  Returns the action name as an atom, raises if unavailable.
  """
  @spec action_name(Plug.Conn.t()) :: atom
  def action_name(conn), do: conn.private.phoenix_action

  @doc """
  Returns the controller module as an atom, raises if unavailable.
  """
  @spec controller_module(Plug.Conn.t()) :: atom
  def controller_module(conn), do: conn.private.phoenix_controller

  @doc """
  Returns the router module as an atom, raises if unavailable.
  """
  @spec router_module(Plug.Conn.t()) :: atom
  def router_module(conn), do: conn.private.phoenix_router

  @doc """
  Returns the endpoint module as an atom, raises if unavailable.
  """
  @spec endpoint_module(Plug.Conn.t()) :: atom
  def endpoint_module(conn), do: conn.private.phoenix_endpoint

  @doc """
  Returns the template name rendered in the view as a string
  (or nil if no template was rendered).
  """
  @spec view_template(Plug.Conn.t()) :: binary | nil
  def view_template(conn) do
    conn.private[:phoenix_template]
  end

  @doc """
  Sends JSON response.

  It uses the configured `:json_library` under the `:phoenix`
  application for `:json` to pick up the encoder module.

  ## Examples

      iex> json(conn, %{id: 123})

  """
  @spec json(Plug.Conn.t(), term) :: Plug.Conn.t()
  def json(conn, data) do
    response = Phoenix.json_library().encode_to_iodata!(data)
    send_resp(conn, conn.status || 200, "application/json", response)
  end

  @doc """
  A plug that may convert a JSON response into a JSONP one.

  In case a JSON response is returned, it will be converted
  to a JSONP as long as the callback field is present in
  the query string. The callback field itself defaults to
  "callback", but may be configured with the callback option.

  In case there is no callback or the response is not encoded
  in JSON format, it is a no-op.

  Only alphanumeric characters and underscore are allowed in the
  callback name. Otherwise an exception is raised.

  ## Examples

      # Will convert JSON to JSONP if callback=someFunction is given
      plug :allow_jsonp

      # Will convert JSON to JSONP if cb=someFunction is given
      plug :allow_jsonp, callback: "cb"

  """
  @spec allow_jsonp(Plug.Conn.t(), Keyword.t()) :: Plug.Conn.t()
  def allow_jsonp(conn, opts \\ []) do
    callback = Keyword.get(opts, :callback, "callback")

    case Map.fetch(conn.query_params, callback) do
      :error ->
        conn

      {:ok, ""} ->
        conn
