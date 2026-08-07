import { z } from 'zod'

// Wire schemas match the backend's DTO layer exactly.
//
// These are defined once and used three ways: runtime validation of responses
// (so a shape mismatch is caught at the boundary rather than deep in a
// component), TypeScript type inference (no separate manual type to keep in
// sync), and documentation that cannot go stale, because a drifting backend
// makes tests fail rather than making a component render undefined.
//
// z.string().datetime() accepts RFC 3339 with an offset, which is exactly what
// the API emits — a naive local timestamp is rejected here rather than being
// silently misinterpreted as the browser's zone.

export const userSchema = z.object({
  id: z.string().uuid(),
  email: z.string().email(),
  name: z.string(),
  role: z.enum(['member', 'admin']),
})

export type User = z.infer<typeof userSchema>

export const roomSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  capacity: z.number().int().positive(),
  amenities: z.array(z.string()),
  available: z.boolean().optional(),
})

export type Room = z.infer<typeof roomSchema>

// Room and user references appear on list responses only. A booking row is
// unreadable without a room name, but the responses to create and check-in go
// back to a caller that already has it on screen — so both are optional here
// rather than pretended-required and defaulted.
//
// `user` additionally only appears for admins. A member listing their own
// bookings has no use for a copy of their own name.
export const bookingRoomRefSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
})

export const bookingUserRefSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  email: z.string().email(),
})

export const bookingSchema = z.object({
  id: z.string().uuid(),
  roomId: z.string().uuid(),
  userId: z.string().uuid(),
  startTime: z.string().datetime(),
  endTime: z.string().datetime(),
  attendeeCount: z.number().int().positive(),
  status: z.enum(['confirmed', 'cancelled', 'released']),
  checkedInAt: z.string().datetime().nullable(),
  createdAt: z.string().datetime(),
  canCheckInNow: z.boolean(),
  canCancel: z.boolean(),
  room: bookingRoomRefSchema.optional(),
  user: bookingUserRefSchema.optional(),
})

export type Booking = z.infer<typeof bookingSchema>

export const intervalSchema = z.object({
  start: z.string().datetime(),
  end: z.string().datetime(),
})

export type Interval = z.infer<typeof intervalSchema>

export const availabilitySchema = z.object({
  room: roomSchema,
  start: z.string().datetime(),
  end: z.string().datetime(),
  busy: z.array(intervalSchema),
  free: z.array(intervalSchema),
})

export type Availability = z.infer<typeof availabilitySchema>

export const utilizationSchema = z.object({
  roomId: z.string().uuid(),
  roomName: z.string(),
  capacity: z.number().int().positive(),
  bookedHours: z.number(),
  bookingCount: z.number().int(),
  noShowCount: z.number().int(),
  utilizationRate: z.number(),
})

export type Utilization = z.infer<typeof utilizationSchema>

// The session token is deliberately absent. It lives in an httpOnly cookie the
// browser attaches automatically, so JavaScript never sees it and there is
// nothing here to accidentally persist.
export const authResponseSchema = z.object({
  user: userSchema,
})

export type AuthResponse = z.infer<typeof authResponseSchema>

// Generic list envelope
export const listResponseSchema = <T extends z.ZodTypeAny>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    nextCursor: z.string().nullable(),
  })

export type ListResponse<T> = {
  items: T[]
  nextCursor: string | null
}

// Error shape
export const apiErrorSchema = z.object({
  error: z.object({
    code: z.string(),
    message: z.string(),
    fields: z.record(z.string()).optional(),
  }),
})

export type ApiError = z.infer<typeof apiErrorSchema>
