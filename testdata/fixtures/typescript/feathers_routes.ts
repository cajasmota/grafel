// Feathers routing fixture (#2851). Hand-written, dependency-manifest-free.
import feathers from '@feathersjs/feathers'

class MessageService {
  async find() {
    return []
  }
  async get(id) {
    return { id }
  }
}

const app = feathers()
app.use('/messages', new MessageService())
app.use('/users', new UserService())
