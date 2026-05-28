// Feathers auth_coverage fixture (#2852). Hand-written.
// Feathers gates services with app-level authentication middleware
// (@feathersjs/authentication). The app.use(authenticate(...)) registration is
// the cross-service auth gate; every service mounted in the app inherits it.
import { feathers } from '@feathersjs/feathers'

const app = feathers()

// App-level authentication hook → file-scope coverage for mounted services.
app.use(authenticate('jwt'))

// Service registrations (REST verb expansion happens in the routing synth).
app.use('/messages', new MessageService())
app.use('/users', new UserService())
