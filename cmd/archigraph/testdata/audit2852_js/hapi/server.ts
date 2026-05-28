// Hapi auth corpus file (#2852 real-data verification).
import Hapi from '@hapi/hapi'

const server = Hapi.server({ port: 4000 })

server.route({
  method: 'GET',
  path: '/private',
  options: { auth: 'session' },
  handler: (request, h) => ({ ok: true }),
})

server.route({
  method: 'POST',
  path: '/login',
  options: { auth: false },
  handler: (request, h) => ({ token: 'x' }),
})
