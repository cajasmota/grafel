// Polka schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level z.object schema is detected as SCOPE.Schema("dto");
// the handler that calls schema.parse emits a dto_extraction VALIDATES edge.
import polka from 'polka'
import { z } from 'zod'

const app = polka()

// zod DTO schema definition
const signupSchema = z.object({
  email: z.string().email(),
  password: z.string().min(8),
})

// handler uses signupSchema.parse → dto_extraction VALIDATES edge
export function signup(req: any, res: any) {
  const data = signupSchema.parse(req.body)
  res.end(JSON.stringify({ ok: true, data }))
}

app.post('/signup', signup)

export default app
