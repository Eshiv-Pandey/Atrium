import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useRegister, useDemoLogin } from '@/api/hooks'
import { ApiError, errorMessage } from '@/api/client'
import { notify } from '@/components/Toast'
import { DemoAccess } from '@/components/DemoAccess'
import { Button } from '@/components/ui/Button'
import { Field } from '@/components/ui/Field'

export function RegisterForm({ redirectTo }: { redirectTo?: string }) {
  const navigate = useNavigate()
  const register = useRegister()
  const demoLogin = useDemoLogin()

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  // Server-reported per-field messages, keyed by field name. The API returns
  // these in the error envelope's `fields`, so a rejected password lands under
  // the password input rather than in a toast the user has to map back to an
  // input themselves.
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const goToDestination = () => {
    void navigate({ to: redirectTo ?? '/rooms' })
  }

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    setFieldErrors({})

    register.mutate(
      { name, email, password },
      {
        onSuccess: goToDestination,
        onError: (err) => {
          if (err instanceof ApiError && err.fields) {
            setFieldErrors(err.fields)
            return
          }
          notify.error('Could not create your account', errorMessage(err))
        },
      },
    )
  }

  const busy = register.isPending || demoLogin.isPending

  return (
    <div className="mx-auto w-full max-w-sm space-y-6">
      <header className="space-y-1 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">
          Create your account
        </h1>
        <p className="text-sm text-muted-fg">
          Already have one?{' '}
          <Link to="/login" className="text-primary hover:underline">
            Sign in
          </Link>
        </p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4" noValidate>
        <Field
          label="Name"
          name="name"
          autoComplete="name"
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          error={fieldErrors.name}
        />
        <Field
          label="Email"
          type="email"
          name="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldErrors.email}
        />
        <Field
          label="Password"
          type="password"
          name="password"
          autoComplete="new-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldErrors.password}
          hint="At least 8 characters."
        />
        <Button
          type="submit"
          className="w-full"
          loading={register.isPending}
          disabled={busy}
        >
          Create account
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
