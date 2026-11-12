import { z } from 'zod'

export const AuthStatusSchema = z.object({
  enabled: z.boolean(),
  login: z.string().min(1).nullable(),
})

export type AuthStatus = z.infer<typeof AuthStatusSchema>
