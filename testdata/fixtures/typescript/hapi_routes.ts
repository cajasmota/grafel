// Hapi routing fixture (#2851). Hand-written, dependency-manifest-free.
import Hapi from '@hapi/hapi'

const server = Hapi.server({ port: 3000 })

function listUsers(request, h) {
  return []
}

server.route({
  method: 'GET',
  path: '/users',
  handler: listUsers,
})

server.route({
  method: 'GET',
  path: '/users/{id}',
  handler: (request, h) => ({ id: request.params.id }),
})

server.route({
  method: ['POST', 'PUT'],
  path: '/users/{id?}',
  handler: function (request, h) {
    return h.response().code(201)
  },
})
