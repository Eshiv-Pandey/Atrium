import { createFileRoute, Link } from '@tanstack/react-router'
import { useSession } from '@/api/hooks'

export const Route = createFileRoute('/')({
  component: Landing,
})

/**
 * The landing page is public and stays public.
 *
 * It is not guarded even for signed-in users: it doubles as the marketing
 * surface, and bouncing someone who deliberately clicked the logo makes the
 * app feel like it is fighting them. The call to action changes instead.
 *
 * This is the plain version. The showpiece treatment — 3D room showcase and an
 * animated CTA — lands here later, and is confined to this route on purpose:
 * animation on a page you see once is a first impression, while animation in a
 * booking tool you use twice a week is friction.
 */
function Landing() {
  const { data: user } = useSession()

  return (
    <div className="space-y-20 py-12">
      <section className="mx-auto max-w-2xl text-center">
        <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl">
          Meeting rooms, without the double-booking
        </h1>
        <p className="mt-4 text-lg text-muted-fg">
          Find a room that fits, reserve it in seconds, and check in when you
          arrive. Rooms nobody shows up for release themselves.
        </p>

        <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
          {user ? (
            <Link
              to="/rooms"
              className="rounded-md bg-primary px-6 py-3 font-medium text-primary-fg hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              Find a room
            </Link>
          ) : (
            <>
              <Link
                to="/login"
                className="rounded-md bg-primary px-6 py-3 font-medium text-primary-fg hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              >
                Sign in
              </Link>
              <Link
                to="/register"
                className="rounded-md border border-border bg-card px-6 py-3 font-medium text-card-fg hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                Create an account
              </Link>
            </>
          )}
        </div>
      </section>

      <section className="mx-auto grid max-w-4xl gap-8 sm:grid-cols-3">
        {features.map((f) => (
          <div key={f.title}>
            <h2 className="font-medium">{f.title}</h2>
            <p className="mt-1 text-sm text-muted-fg">{f.body}</p>
          </div>
        ))}
      </section>
    </div>
  )
}

const features = [
  {
    title: 'Filter by what you need',
    body: 'Capacity and amenities, narrowed to the rooms actually free in your window.',
  },
  {
    title: 'One booking per slot, guaranteed',
    body: 'Two people booking the same room at the same moment: one succeeds, one is told immediately.',
  },
  {
    title: 'No-shows free themselves',
    body: 'Check in from five minutes before. Fifteen minutes past the start with nobody there, the room goes back on the board.',
  },
]
