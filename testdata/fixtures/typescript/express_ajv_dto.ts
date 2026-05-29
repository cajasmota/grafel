// Express AJV schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level ajv.compile call is detected as SCOPE.Schema("dto");
// the handler that calls the compiled validate function demonstrates AJV DTO usage.
// Note: AJV's compile() returns a validate function, so this fixture uses the
// ajv.compile pattern to prove AJV schema detection.
import express from 'express'
import Ajv from 'ajv'

const app = express()

const ajv = new Ajv()

// AJV schema definition via ajv.compile → detected as SCOPE.Schema("dto")
const validateCreateUser = ajv.compile({
  type: 'object',
  properties: {
    name: { type: 'string' },
    age: { type: 'number' },
  },
  required: ['name'],
})

// handler calls validateCreateUser which was produced by ajv.compile
export function createUser(req: any, res: any) {
  const valid = validateCreateUser(req.body)
  if (!valid) return res.status(400).json({ errors: validateCreateUser.errors })
  res.status(201).json(req.body)
}

app.post('/users', createUser)
