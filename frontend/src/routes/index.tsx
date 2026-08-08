import { createFileRoute, Link } from '@tanstack/react-router'
import { useSession } from '@/api/hooks'

export const Route = createFileRoute('/')({
  component: Landing,
})

/** The claims that scroll in the ticker. Duplicated in the track, not here. */
const TICKER = [
  'One booking per slot',
  'Enforced in Postgres',
  'No-shows release themselves',
  'Back-to-back is legal',
  'Half-open intervals',
  'Idempotent submits',
]

const FEATURES = [
  {
    n: '01',
    title: 'One slot, one booking',
    body: 'Two people hitting confirm on the same room at the same moment is settled by a database exclusion constraint, not by whoever the server heard from first. The loser is told the slot went, immediately.',
  },
  {
    n: '02',
    title: 'No-shows give the room back',
    body: 'Fifteen minutes past your start with no check-in and the reservation drops off the board. There is no sweeper process to fall behind: the release happens inside the next booking on that room.',
  },
  {
    n: '03',
    title: 'Back-to-back actually works',
    body: 'A meeting ending at 11:00 and one starting at 11:00 do not overlap. Slots are half-open, so the hour after yours is bookable rather than mysteriously blocked.',
  },
]

function Landing() {
  const { data: user } = useSession()

  return (
    <div className="flex flex-col gap-24 pb-8">
      <Hero signedIn={Boolean(user)} />
      <Ticker />
      <Features />
      <ClosingCta signedIn={Boolean(user)} />
    </div>
  )
}

function Hero({ signedIn }: { signedIn: boolean }) {
  return (
    <section className="relative isolate pt-6 sm:pt-12">
      {/* Ghosted lettering behind the headline — the layered-type device from
          the reference art. Purely texture, so it is hidden from assistive
          tech and never intercepts a click on the buttons above it. */}
      <span
        aria-hidden="true"
        className="ghost-type fade-b pointer-events-none absolute -top-4 left-0 -z-10 select-none whitespace-nowrap text-[7rem] sm:text-[13rem] lg:text-[17rem]"
      >
        Atrium
      </span>

      <p className="micro animate-rise-in">Co-working · Meeting rooms</p>

      <h1 className="display-xl mt-5 animate-rise-in text-[3.25rem] sm:text-display-lg lg:text-display-xl">
        Book the room.
        <br />
        {/* The one line set in cream rather than ember: the promise is the
            product, so it gets the brightest value on the page. */}
        <span className="text-accent">Keep the room.</span>
      </h1>

      <p className="mt-7 max-w-xl text-pretty text-lg leading-relaxed text-muted-fg sm:text-xl">
        Meeting rooms for a co-working space, where a confirmed booking is
        actually yours. Double-booking is impossible at the database level, and
        rooms nobody turns up for come back automatically.
      </p>

      <div className="mt-10 flex flex-wrap items-center gap-3">
        {signedIn ? (
          <>
            <CtaPrimary to="/rooms">Browse rooms</CtaPrimary>
            <CtaGhost to="/bookings">My bookings</CtaGhost>
          </>
        ) : (
          <>
            <CtaPrimary to="/login">Sign in to book</CtaPrimary>
            <CtaGhost to="/rooms">See the rooms</CtaGhost>
          </>
        )}
      </div>
    </section>
  )
}

function CtaPrimary({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className="rounded-full bg-primary px-7 py-3.5 font-semibold text-primary-fg transition-transform hover:scale-[1.03] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      {children}
    </Link>
  )
}

function CtaGhost({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className="rounded-full border border-foreground/25 px-7 py-3.5 font-semibold text-foreground transition-colors hover:border-foreground/50 hover:bg-foreground/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {children}
    </Link>
  )
}

/**
 * Scrolling claim strip.
 *
 * The track holds the list twice and translates by exactly -50%, so the loop
 * lands where the first copy ended and there is no visible snap. Hidden from
 * assistive tech because the duplication would otherwise be announced twice —
 * every claim here is stated properly in the section below.
 */
function Ticker() {
  return (
    <section
      aria-hidden="true"
      className="relative -mx-4 overflow-hidden border-y border-border/60 bg-accent py-4 sm:-mx-6"
    >
      <div className="flex w-max animate-marquee">
        {[0, 1].map((copy) => (
          <ul key={copy} className="flex shrink-0 items-center">
            {TICKER.map((claim) => (
              <li
                key={claim}
                className="flex items-center gap-6 whitespace-nowrap px-6 font-display text-sm font-bold uppercase tracking-[0.16em] text-accent-fg"
              >
                {claim}
                <span className="text-primary">✦</span>
              </li>
            ))}
          </ul>
        ))}
      </div>
    </section>
  )
}

function Features() {
  return (
    <section className="ember-wash-b relative rounded-2xl">
      <h2 className="display-xl text-display-sm sm:text-display-md">
        Three rules,
        <br />
        <span className="text-primary">enforced not promised.</span>
      </h2>

      <ol className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border/70 bg-border/70 sm:grid-cols-3">
        {FEATURES.map((f) => (
          <li key={f.n} className="flex flex-col gap-4 bg-card/80 p-7 backdrop-blur-sm">
            <span className="font-mono text-sm font-medium text-primary">{f.n}</span>
            <h3 className="font-display text-xl font-bold uppercase tracking-[-0.01em]">
              {f.title}
            </h3>
            <p className="text-pretty text-sm leading-relaxed text-muted-fg">{f.body}</p>
          </li>
        ))}
      </ol>
    </section>
  )
}

/**
 * Closing panel, inverted to cream. The colour flip is the point: after a long
 * dark page it reads as the end of the document rather than as one more card.
 */
function ClosingCta({ signedIn }: { signedIn: boolean }) {
  return (
    <section className="relative isolate overflow-hidden rounded-2xl bg-accent px-7 py-14 text-accent-fg sm:px-12 sm:py-20">
      <span
        aria-hidden="true"
        className="ghost-type ghost-type-solid pointer-events-none absolute -bottom-6 -right-4 -z-10 select-none text-[6rem] text-accent-fg/10 sm:text-[10rem]"
      >
        Book
      </span>

      <p className="micro text-accent-fg/60">Ready when you are</p>

      <h2 className="display-xl mt-4 max-w-2xl text-balance text-display-sm sm:text-display-md">
        Find a room for the next hour.
      </h2>

      <div className="mt-9">
        <Link
          to={signedIn ? '/rooms' : '/login'}
          className="inline-block rounded-full bg-accent-fg px-8 py-4 font-semibold text-accent transition-transform hover:scale-[1.03] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-fg focus-visible:ring-offset-2 focus-visible:ring-offset-accent"
        >
          {signedIn ? 'Browse rooms' : 'Sign in to book'}
        </Link>
      </div>
    </section>
  )
}
