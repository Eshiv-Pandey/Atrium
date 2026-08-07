import { Button } from '@/components/ui/Button'

/**
 * DemoAccess is the reviewer's one-click path into the product.
 *
 * Shared by both auth screens so the two cannot drift on wording or on which
 * roles are offered. The endpoint behind it is mounted only when
 * DEMO_LOGIN_ENABLED is set, so these buttons 404 on a deployment that has not
 * opted in — which is why a failure reads as "unavailable" rather than as a
 * rejected sign-in.
 */
export function DemoAccess({
  onSelect,
  disabled,
}: {
  onSelect: (role: 'member' | 'admin') => void
  disabled: boolean
}) {
  return (
    <div className="space-y-3">
      <div className="relative">
        <div className="absolute inset-0 flex items-center" aria-hidden="true">
          <div className="w-full border-t border-border" />
        </div>
        <div className="relative flex justify-center">
          <span className="bg-background px-2 text-xs uppercase tracking-wide text-muted-fg">
            Or explore as
          </span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <Button
          variant="secondary"
          disabled={disabled}
          onClick={() => onSelect('member')}
        >
          Member
        </Button>
        <Button
          variant="secondary"
          disabled={disabled}
          onClick={() => onSelect('admin')}
        >
          Admin
        </Button>
      </div>

      <p className="text-center text-xs text-muted-fg">
        Demo accounts skip the password. They are gated behind an environment
        flag that is off by default.
      </p>
    </div>
  )
}
