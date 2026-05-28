// Restify routing fixture (#2851). Hand-written, dependency-manifest-free.
import restify from 'restify'

function listUsers(req, res, next) {
  res.send([])
  next()
}

const server = restify.createServer()
server.get('/users', listUsers)
server.get('/users/:id', (req, res, next) => {
  res.send({ id: req.params.id })
  next()
})
server.post('/users', listUsers)
server.del('/users/:id', (req, res, next) => {
  res.send(204)
  next()
})
server.listen(8080)
