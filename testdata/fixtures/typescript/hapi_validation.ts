// hapi request_validation fixture (#2904). Hand-written, dependency-free.
// A named hapi handler validates the payload with Joi (Joi.attempt).
import Hapi from '@hapi/hapi'
import Joi from 'joi'

const server = Hapi.server({ port: 3000 })

const payloadSchema = Joi.object({ sku: Joi.string() })

export function createItem(request: any, h: any) {
  const value = Joi.attempt(request.payload, payloadSchema)
  return h.response(value)
}

server.route({ method: 'POST', path: '/items', handler: createItem })
