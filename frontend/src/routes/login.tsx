import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'
import { sessionQueryOptions } from '@/api/hooks'
import { LoginForm } from '@/components/LoginForm'

/**
 * The `redirect` param is validated, not just read.
 *
 * It is attacker-controllable — anyone can send a link to
 * /login?redirect=https://evil.example — and it is fed straight to a
 * navigation on success. Constraining it to a path beginning with a single
 * slash keeps it inside this app. `//evil.example` is rejected too: browsers
 * read a protocol-relative URL as an absolute one, so allowing it would be the
 * same open redirect with two extra characters.
 */
const searchSchema = z.object({
  redirect: z
    .string()
    .refine((v) => v.startsWith('/') && !v.startsWith('//'), {
      message: 'redirect must be a path within this app',
    })
    .optional()
    .catch(undefined),
})

export const Route = createFileRoute('/login')({
  validateSearch: searchSchema,

  // Someone already signed in has no use for this page — most arrive by
  // pressing Back after logging in. Bouncing them forward is friendlier than
  // showing a form that would sign them in as the person they already are.
  beforeLoad: async ({ context, search }) => {
    const user = await context.queryClient.ensureQueryData(sessionQueryOptions)
    if (user) {
      throw redirect({ to: search.redirect ?? '/rooms' })
    }
  },

  component: LoginPage,
})

function LoginPage() {
  const { redirect: redirectTo } = Route.useSearch()
  return (
    <div className="relative isolate py-10 sm:py-16">
      {/* Same layered-type device as the landing hero, scaled down. Decorative
          only, so it is hidden from assistive tech and cannot swallow a click
          on the form in front of it. */}
      <span
        aria-hidden="true"
        className="ghost-type fade-b pointer-events-none absolute -top-2 left-1/2 -z-10 -translate-x-1/2 select-none text-[6rem] sm:text-[9rem]"
      >
        Atrium
      </span>
      <LoginForm redirectTo={redirectTo} />
    </div>
  )
}
