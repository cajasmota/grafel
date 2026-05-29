// Express schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. Top-level z.object / Joi.object declarations are detected
// as SCOPE.Schema("dto") entities; when a handler parses the schema variable,
// a VALIDATES edge with via=dto_extraction is emitted (handler → dto:<name>).
import express from 'express'
import { z } from 'zod'
import Joi from 'joi'

const app = express()

// zod schema definition — detected as SCOPE.Schema("dto")
const createUserSchema = z.object({
  name: z.string(),
  age: z.number(),
})

// joi schema definition — detected as SCOPE.Schema("dto")
const updateUserSchema = Joi.object({
  name: Joi.string(),
  email: Joi.string().email(),
})

// handler uses createUserSchema.parse → dto_extraction VALIDATES edge
export function createUser(req: any, res: any) {
  const data = createUserSchema.parse(req.body)
  res.status(201).json(data)
}

// handler uses updateUserSchema.validate → dto_extraction VALIDATES edge
export function updateUser(req: any, res: any) {
  const { error, value } = updateUserSchema.validate(req.body)
  if (error) return res.status(400).json({ error: error.message })
  res.json(value)
}

app.post('/users', createUser)
app.put('/users/:id', updateUser)
