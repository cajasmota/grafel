<script setup lang="ts">
// Cross-framework idioms inside a Vue SFC (#2910):
//  - @tanstack/vue-query useQuery / useMutation / useInfiniteQuery
//  - framework-agnostic Redux Toolkit configureStore / createSlice
import { useQuery, useMutation, useInfiniteQuery } from '@tanstack/vue-query'
import { configureStore, createSlice } from '@reduxjs/toolkit'

const todos = useQuery({ queryKey: ['todos'], queryFn: fetchTodos })
const addTodo = useMutation({ mutationFn: postTodo })
const pages = useInfiniteQuery({ queryKey: ['feed'], queryFn: fetchPage })

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

<template>
  <div>{{ todos.data }}</div>
</template>
