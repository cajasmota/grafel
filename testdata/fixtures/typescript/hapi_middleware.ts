// Hapi middleware_coverage fixture (#2853). Hand-written, manifest-free.
// Hapi expresses middleware through server extension points (server.ext) and
// per-route options.pre / options.ext.
import Hapi from '@hapi/hapi'

const server = Hapi.server({ port: 3000 })

// Server-wide extension point — applies to every route.
server.ext('onPreHandler', enrichContext)

server.route({
  method: 'GET',
  path: '/private',
  options: {
    pre: [{ method: loadUser }],
  },
  handler: getPrivate,
})

server.route({
  method: 'POST',
  path: '/login',
  handler: login,
})
