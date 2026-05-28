// Polka routing fixture (#2851). Hand-written, dependency-manifest-free.
import polka from 'polka'

function listUsers(req, res) {
  res.end('[]')
}

const app = polka()
app.get('/users', listUsers)
app.get('/users/:id', (req, res) => res.end('{}'))
app.post('/users', (req, res) => res.end('{}'))
app.listen(3000)
