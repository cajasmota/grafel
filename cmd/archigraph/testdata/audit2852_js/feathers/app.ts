// Feathers auth corpus file (#2852 real-data verification).
import { feathers } from '@feathersjs/feathers'

const app = feathers()
app.use(authenticate('jwt'))
app.use('/messages', new MessageService())
