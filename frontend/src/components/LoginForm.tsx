import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useLogin, useDemoLogin } from '@/api/hooks'
import { errorMessage } from '@/api/client'
import { notify } from '@/components/Toast'
import { DemoAccess } from '@/components/DemoAccess'
import { Button } from '@/components/ui/Button'
import { Field } from '@/components/ui/Field'

/**
 * `redirectTo` comes from the route guard, which records where the user was
 * heading before it bounced them here. Sending everyone to /rooms instead
 * would silently discard a deep link — someone following a URL to a specific
 * room would sign in and land on a list, with no indication of what happened.
 */
export function LoginForm({ redirectTo }: { redirectTo?: string }) {
  const navigate = useNavigate()
  const login = useLogin()
  const demoLogin = useDemoLogin()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  const goToDestination = () => {
    void navigate({ to: redirectTo ?? '/rooms' })
  }

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: goToDestination,
        // A failed sign-in is reported as one message, not per-field. The
        // server deliberately cannot say which of the two was wrong: telling
        // them apart would let anyone test whether an address has an account.
        onError: (err) => notify.error('Could not sign in', errorMessage(err)),
      },
    )
  }

  const busy = login.isPending || demoLogin.isPending

  return (
    <div className="mx-auto w-full max-w-sm space-y-6">
      <header className="space-y-1 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">Welcome back</h1>
        <p className="text-sm text-muted-fg">
          New here?{' '}
          <Link to="/register" className="text-primary hover:underline">
            Create an account
          </Link>
        </p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4" noValidate>
        <Field
          label="Email"
          type="email"
          name="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Field
          label="Password"
          type="password"
          name="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button
          type="submit"
          className="w-full"
          loading={login.isPending}
          disabled={busy}
        >
          Sign in
        </Button>
      </form>

      <DemoAccess
        disabled={busy}
        onSelect={(role) =>
          demoLogin.mutate(role, {
            onSuccess: goToDestination,
            onError: (err) =>
              notify.error('Demo sign-in unavailable', errorMessage(err)),
          })
        }
      />
    </div>
  )
}
