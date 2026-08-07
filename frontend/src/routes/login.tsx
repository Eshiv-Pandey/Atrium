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
    <div className="py-12">
      <LoginForm redirectTo={redirectTo} />
    </div>
  )
}
