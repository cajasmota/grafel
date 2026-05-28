// Hapi auth_coverage fixture (#2852). Hand-written.
// Per-route options.auth strategy → protected; auth: false → explicit public.
const server = Hapi.server({ port: 3000 })

server.route({
  method: 'GET',
  path: '/private',
  options: { auth: 'jwt' },
  handler: getPrivate,
})

server.route({
  method: 'POST',
  path: '/login',
  options: { auth: false },
  handler: login,
})
