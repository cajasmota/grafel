// AdonisJS request_validation fixture (#2904). Hand-written, dependency-free.
// AdonisJS controllers validate request input with a zod schema.
import { z } from 'zod'

const storeSchema = z.object({ email: z.string(), name: z.string() })

export default class PostsController {
  public async store({ request, response }: any) {
    const data = storeSchema.parse(request.all())
    return response.json(data)
  }
}
