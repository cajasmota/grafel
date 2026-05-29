// Hono schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level z.object schema is detected as SCOPE.Schema("dto");
// the handler that calls schema.parse emits a dto_extraction VALIDATES edge.
import { Hono } from 'hono'
import { z } from 'zod'

const app = new Hono()

// zod DTO schema definition
const createTodoSchema = z.object({
  title: z.string().min(1),
  done: z.boolean().default(false),
})

// handler uses createTodoSchema.parse → dto_extraction VALIDATES edge
export async function createTodo(c: any) {
  const body = await c.req.json()
  const todo = createTodoSchema.parse(body)
  return c.json(todo, 201)
}

app.post('/todos', createTodo)

export default app
