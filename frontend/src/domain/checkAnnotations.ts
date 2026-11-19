import { z } from 'zod'

const webURL = z.url({ protocol: /^https?$/ })
const fullSha = z.string().regex(/^[0-9a-f]{40}$/)

export const CheckAnnotationSchema = z.object({
  path: z.string(), startLine: z.number().int(), endLine: z.number().int(),
  level: z.string(), title: z.string().optional(), message: z.string(),
  details: z.string().optional(), sourceUrl: webURL.optional(),
})

export const CheckAnnotationPageSchema = z.object({
  revisionSha: fullSha,
  checkRunId: z.number().int().positive(),
  checkRunUrl: webURL.optional(),
  page: z.number().int().positive(),
  pageSize: z.number().int().positive(),
  hasMore: z.boolean(),
  items: z.array(CheckAnnotationSchema),
})

export type CheckAnnotation = z.infer<typeof CheckAnnotationSchema>
export type CheckAnnotationPage = z.infer<typeof CheckAnnotationPageSchema>
