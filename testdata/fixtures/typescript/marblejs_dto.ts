// Marble.js schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level z.object schema is detected as SCOPE.Schema("dto");
// the effect function that calls schema.parse emits a dto_extraction VALIDATES edge.
import { z } from 'zod'

// zod DTO schema definition
const createPostSchema = z.object({
  title: z.string().min(1),
  content: z.string(),
  authorId: z.number().int(),
})

// effect uses createPostSchema.parse → dto_extraction VALIDATES edge
export const createPostEffect$ = (req: any) => {
  const data = createPostSchema.parse(req.body)
  return { status: 201, body: data }
}
