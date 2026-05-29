// Feathers schema-library DTO extraction fixture (#3073). Hand-written,
// dependency-free. A top-level yup.object schema is detected as SCOPE.Schema("dto");
// a service hook that calls schema.validate emits a dto_extraction VALIDATES edge.
import * as yup from 'yup'

// yup DTO schema definition
const createMessageSchema = yup.object({
  text: yup.string().required().max(500),
  channelId: yup.number().integer().required(),
})

// hook uses createMessageSchema.validate → dto_extraction VALIDATES edge
export const validateCreateMessage = async (context: any) => {
  const validated = await createMessageSchema.validate(context.data)
  context.data = validated
  return context
}
