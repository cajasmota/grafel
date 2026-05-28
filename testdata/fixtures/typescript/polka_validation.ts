// Polka request_validation fixture (#2904). Hand-written, dependency-free.
// A named Polka route handler validates req.body with a zod schema.
import polka from 'polka'
import { z } from 'zod'

const app = polka()

const signupSchema = z.object({ email: z.string() })

export function signup(req: any, res: any) {
  const data = signupSchema.parse(req.body)
  res.end(JSON.stringify(data))
}

app.post('/signup', signup)

export default app
