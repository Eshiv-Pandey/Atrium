import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'
import { sessionQueryOptions } from '@/api/hooks'
import { RegisterForm } from '@/components/RegisterForm'

// Same validation as /login — see the note there on why an unchecked redirect
// param is an open redirect.
const searchSchema = z.object({
  redirect: z
    .string()
    .refine((v) => v.startsWith('/') && !v.startsWith('//'), {
      message: 'redirect must be a path within this app',
    })
    .optional()
    .catch(undefined),
})

export const Route = createFileRoute('/register')({
  validateSearch: searchSchema,

  beforeLoad: async ({ context, search }) => {
    const user = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (user) {
      throw redirect({ to: search.redirect ?? '/rooms' })
    }
  },

  component: RegisterPage,
})

function RegisterPage() {
  const { redirect: redirectTo } = Route.useSearch()
  return (
    <div className="relative isolate py-10 sm:py-16">
      {/* See /login — the same decorative lettering, kept identical so the two
          auth screens read as one surface. */}
      <span
        aria-hidden="true"
        className="ghost-type fade-b pointer-events-none absolute -top-2 left-1/2 -z-10 -translate-x-1/2 select-none text-[6rem] sm:text-[9rem]"
      >
        Atrium
      </span>
      <RegisterForm redirectTo={redirectTo} />
    </div>
  )
}
