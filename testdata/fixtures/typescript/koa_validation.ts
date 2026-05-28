// Koa request_validation fixture (#2904). Hand-written, dependency-free.
// A named Koa middleware validates ctx.request.body with a joi schema.
import Koa from 'koa'
import Joi from 'joi'

const app = new Koa()

const bodySchema = Joi.object({ title: Joi.string(), qty: Joi.number() })

export async function validateBody(ctx: any) {
  const result = bodySchema.validate(ctx.request.body)
  ctx.body = { ok: !result.error }
}

app.use(validateBody)
