// Sails schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. Top-level z.object / yup.object schemas are detected as
// SCOPE.Schema("dto") entities; the action that calls schema.parse/validateSync
// emits dto_extraction VALIDATES edges.
import { z } from 'zod'
import * as yup from 'yup'

// zod DTO schema definition
const createUserSchema = z.object({
  name: z.string().min(1),
  email: z.string().email(),
})

// yup DTO schema definition (AJV pattern via compile-like validateSync)
const updateUserSchema = yup.object({
  name: yup.string(),
  email: yup.string().email(),
})

// action uses createUserSchema.parse → dto_extraction VALIDATES edge
export async function create(req: any, res: any) {
  const data = createUserSchema.parse(req.allParams())
  return res.json({ created: data })
}

// action uses updateUserSchema.validateSync → dto_extraction VALIDATES edge
export async function update(req: any, res: any) {
  const data = updateUserSchema.validateSync(req.allParams())
  return res.json({ updated: data })
}
