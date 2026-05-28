// Restify request_validation fixture (#2904). Hand-written, dependency-free.
// A named Restify route handler validates the request body with a joi schema.
import * as restify from 'restify'
import Joi from 'joi'

const server = restify.createServer()

const orderSchema = Joi.object({ item: Joi.string(), qty: Joi.number() })

export function createOrder(req: any, res: any, next: any) {
  const result = orderSchema.validate(req.body)
  res.send(result.error ? 400 : 201)
  return next()
}

server.post('/orders', createOrder)
