// Koa schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level yup.object schema is detected as SCOPE.Schema("dto");
// the middleware that calls schema.validate emits a dto_extraction VALIDATES edge.
import Koa from 'koa'
import * as yup from 'yup'

const app = new Koa()

// yup DTO schema definition
const createItemSchema = yup.object({
  title: yup.string().required(),
  qty: yup.number().integer().min(1),
})

// middleware uses createItemSchema.validate → dto_extraction VALIDATES edge
export async function createItem(ctx: any) {
  const validated = await createItemSchema.validate(ctx.request.body)
  ctx.body = { item: validated }
  ctx.status = 201
}

app.use(createItem)
