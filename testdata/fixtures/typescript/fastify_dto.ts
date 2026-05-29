// Fastify schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level z.object schema is detected as SCOPE.Schema("dto");
// the handler that calls schema.parse emits a dto_extraction VALIDATES edge.
import Fastify from 'fastify'
import { z } from 'zod'

const app = Fastify()

// zod DTO schema definition
const loginSchema = z.object({
  username: z.string(),
  password: z.string().min(8),
})

// handler uses loginSchema.safeParse → dto_extraction VALIDATES edge
export async function login(request: any, reply: any) {
  const result = loginSchema.safeParse(request.body)
  if (!result.success) return reply.code(400).send(result.error)
  return reply.send({ ok: true })
}

app.post('/login', login)
