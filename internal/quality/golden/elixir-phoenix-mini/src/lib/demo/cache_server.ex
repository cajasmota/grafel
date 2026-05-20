defmodule Demo.CacheServer do
  @moduledoc """
  A GenServer that caches user lookup results in memory.
  Demonstrates OTP callback patterns: init/1, handle_call/3,
  handle_cast/2, handle_info/2.
  """

  use GenServer

  ## Client API

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, opts)
  end

  def get(server, key) do
    GenServer.call(server, {:get, key})
  end

  def put(server, key, value) do
    GenServer.cast(server, {:put, key, value})
  end

  def flush(server) do
    GenServer.cast(server, :flush)
  end

  ## Server Callbacks

  def init(state) do
    {:ok, state}
  end

  def handle_call({:get, key}, _from, state) do
    value = Map.get(state, key)
    {:reply, value, state}
  end

  def handle_cast({:put, key, value}, state) do
    new_state = Map.put(state, key, value)
    {:noreply, new_state}
  end

  def handle_cast(:flush, _state) do
    {:noreply, %{}}
  end

  def handle_info({:expire, key}, state) do
    new_state = Map.delete(state, key)
    {:noreply, new_state}
  end

  def terminate(reason, _state) do
    require Logger
    Logger.debug("CacheServer terminating: #{inspect(reason)}")
    :ok
  end
end
