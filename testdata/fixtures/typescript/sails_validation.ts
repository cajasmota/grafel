// Sails request_validation fixture (#2904). Hand-written, dependency-free.
// A named Sails controller action validates the request params with a yup
// schema. Sails actions-as-functions (the modern Sails idiom) are named
// exported functions, which the extractor lifts to an operation entity.
import * as yup from 'yup'

const createSchema = yup.object({ name: yup.string(), email: yup.string() })

export async function create(req: any, res: any) {
  const validated = createSchema.validateSync(req.allParams())
  return res.json(validated)
}
