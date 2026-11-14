import { z } from 'zod'

export const GhostlineSessionSchema = z.object({
  authRequired: z.boolean(),
  authenticated: z.boolean(),
  login: z.string().optional(),
}).superRefine((session, context) => {
  if (session.authRequired && session.authenticated && !session.login) {
    context.addIssue({ code: 'custom', path: ['login'], message: 'Authenticated sessions must identify their GitHub account.' })
  }
  if (session.authRequired && !session.authenticated && session.login !== undefined) {
    context.addIssue({ code: 'custom', path: ['login'], message: 'An unauthenticated session cannot contain a GitHub account.' })
  }
})

export type GhostlineSession = z.infer<typeof GhostlineSessionSchema>
