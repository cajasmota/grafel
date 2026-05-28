// Feathers request_validation fixture (#2904). Hand-written, dependency-free.
// A Feathers service hook validates the incoming data with a yup schema.
import * as yup from 'yup'

const messageSchema = yup.object({ text: yup.string() })

export const validateMessage = async (context: any) => {
  const validated = messageSchema.validate(context.data)
  context.data = validated
  return context
}
