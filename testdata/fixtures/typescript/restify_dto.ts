// Restify schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level Joi.object schema is detected as SCOPE.Schema("dto");
// the handler that calls schema.validate emits a dto_extraction VALIDATES edge.
import * as restify from 'restify'
import Joi from 'joi'

const server = restify.createServer()

// joi DTO schema definition
const createOrderSchema = Joi.object({
  item: Joi.string().required(),
  qty: Joi.number().integer().min(1),
})

// handler uses createOrderSchema.validate → dto_extraction VALIDATES edge
export function createOrder(req: any, res: any, next: any) {
  const { error, value } = createOrderSchema.validate(req.body)
  if (error) {
    res.send(400, { error: error.message })
  } else {
    res.send(201, value)
  }
  return next()
}

server.post('/orders', createOrder)
