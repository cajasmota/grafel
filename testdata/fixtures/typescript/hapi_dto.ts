// Hapi schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level Joi.object schema is detected as SCOPE.Schema("dto");
// the handler that calls schema.validate emits a dto_extraction VALIDATES edge.
import Hapi from '@hapi/hapi'
import Joi from 'joi'

const server = Hapi.server({ port: 3000 })

// joi DTO schema definition
const createOrderSchema = Joi.object({
  sku: Joi.string().required(),
  qty: Joi.number().integer().min(1),
})

// handler uses createOrderSchema.validate → dto_extraction VALIDATES edge
export function createOrder(request: any, h: any) {
  const { error, value } = createOrderSchema.validate(request.payload)
  if (error) return h.response({ error: error.message }).code(400)
  return h.response(value).code(201)
}

server.route({ method: 'POST', path: '/orders', handler: createOrder })
