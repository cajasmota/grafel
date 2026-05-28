// audit2853_js — Hapi slice. Server ext point + per-route options.pre.
// Route shapes mirror the proven #2852 auth corpus (flat options object) so the
// full indexer pipeline synthesizes both routes.
import Hapi from '@hapi/hapi'

const server = Hapi.server({ port: 4000 })

server.ext('onPreHandler', (request, h) => h.continue)

server.route({
  method: 'GET',
  path: '/private',
  options: { pre: [loadUser] },
  handler: (request, h) => ({ ok: true }),
})

server.route({
  method: 'POST',
  path: '/login',
  handler: (request, h) => ({ token: 'x' }),
})
