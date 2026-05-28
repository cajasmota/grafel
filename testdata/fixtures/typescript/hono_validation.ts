// Hono request_validation fixture (#2904). Hand-written, dependency-free.
// A named Hono route handler validates the parsed JSON body with a zod schema.
import { Hono } from 'hono'
import { z } from 'zod'

const app = new Hono()

const todoSchema = z.object({ title: z.string(), done: z.boolean() })

export async function createTodo(c: any) {
  const body = await c.req.json()
  const todo = todoSchema.parse(body)
  return c.json(todo)
}

app.post('/todos', createTodo)

export default app
