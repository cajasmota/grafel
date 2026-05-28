<script lang="ts">
  // Cross-framework idioms inside a Svelte component (#2910):
  //  - @tanstack/svelte-query createQuery / createMutation / createInfiniteQuery
  //  - framework-agnostic Redux Toolkit createSlice / configureStore
  import { createQuery, createMutation, createInfiniteQuery } from '@tanstack/svelte-query'
  import { configureStore, createSlice } from '@reduxjs/toolkit'

  const todos = createQuery({ queryKey: ['todos'], queryFn: fetchTodos })
  const addTodo = createMutation({ mutationFn: postTodo })
  const feed = createInfiniteQuery({ queryKey: ['feed'], queryFn: fetchPage })

  const counterSlice = createSlice({
    name: 'counter',
    initialState: { value: 0 },
    reducers: {
      increment(state) { state.value += 1 },
    },
  })

  const store = configureStore({ reducer: counterSlice.reducer })

  async function fetchTodos() { return [] }
  async function postTodo() { return {} }
  async function fetchPage() { return [] }
</script>

<div>{$todos.data}</div>
