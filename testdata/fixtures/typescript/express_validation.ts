// Express request_validation fixture (#2904). Hand-written, dependency-free.
// Named Express handlers validate the request body with both zod and
// express-validator, so the route↔validator linkage (VALIDATES edge) is
// proven for two libraries on the same framework. Routes wire the named
// handlers (app.post('/users', createUser)), the idiom the extractor
// attributes edges to.
import express from 'express'
import { z } from 'zod'
import { validationResult } from 'express-validator'

const app = express()

const createUserSchema = z.object({ name: z.string(), age: z.number() })

// zod: schema.parse(req.body) inside the handler → VALIDATES validator:zod.
export function createUser(req: any, res: any) {
  const parsed = createUserSchema.parse(req.body)
  res.json(parsed)
}

// express-validator: validationResult(req) inside the handler →
// VALIDATES validator:express-validator.
export function updateUser(req: any, res: any) {
  const errors = validationResult(req)
  res.json({ ok: errors.isEmpty() })
}

app.post('/users', createUser)
app.put('/users/:id', updateUser)
