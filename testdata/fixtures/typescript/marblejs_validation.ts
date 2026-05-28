// Marble.js request_validation fixture (#2904). Hand-written, dependency-free.
// A Marble.js effect validates the request body with a joi schema.
import Joi from 'joi'

const createSchema = Joi.object({ name: Joi.string() })

export const createEffect$ = (req: any) => {
  const result = createSchema.validate(req.body)
  return { status: result.error ? 400 : 200 }
}
