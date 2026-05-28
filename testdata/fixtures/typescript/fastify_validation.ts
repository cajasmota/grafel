// Fastify request_validation fixture (#2904). Hand-written, dependency-free.
// A named Fastify route handler validates the incoming body with a zod schema.
import Fastify from 'fastify'
import { z } from 'zod'

const app = Fastify()

const loginSchema = z.object({ user: z.string(), pass: z.string() })

export async function login(request: any, reply: any) {
  const creds = loginSchema.safeParse(request.body)
  return reply.send({ ok: creds.success })
}

app.post('/login', login)
