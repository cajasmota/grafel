// Restify auth_coverage fixture (#2852). Hand-written.
import restify from 'restify'

const server = restify.createServer()

// App-level auth gate via server.use(...).
server.use(passport.authenticate('jwt'))

// Route-level guard (high confidence).
server.get('/secrets', requireAuth, getSecrets)

// Inherits the server.use passport gate (medium).
server.get('/info', getInfo)
