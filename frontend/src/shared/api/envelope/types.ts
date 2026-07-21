import { z } from 'zod'

const detailsSchema = z.object({ field: z.string().optional(), code: z.string(), message: z.string() })
export const envelopeSchema = z.discriminatedUnion('success', [
  z.object({ success: z.literal(true), data: z.unknown(), meta: z.object({ request_id: z.string() }) }),
  z.object({ success: z.literal(false), error: z.object({ code: z.string(), message: z.string(), details: z.array(detailsSchema).optional() }), meta: z.object({ request_id: z.string() }) }),
])
export type ApiMeta = { request_id: string }
export type ApiError = { code: string; message: string; details?: Array<{ field?: string; code: string; message: string }> }
export type ApiSuccess<T> = { success: true; data: T; meta: ApiMeta }
export type ApiFailure = { success: false; error: ApiError; meta: ApiMeta }
export type ApiEnvelope<T> = ApiSuccess<T> | ApiFailure
