import { cleanup, screen } from '@testing-library/react'
import type { Room } from '@/api/schemas'
import { renderWithRouter } from '@/test/router'
import { RoomCard, formatAmenity } from './RoomCard'

const ROOM_ID = '11111111-1111-4111-8111-111111111111'

function room(overrides: Partial<Room> = {}): Room {
  return {
    id: ROOM_ID,
    name: 'Aurora',
    capacity: 8,
    amenities: ['tv', 'whiteboard'],
    ...overrides,
  }
}

describe('RoomCard availability badge', () => {
  // The tri-state, one case each. `available` is absent until the user picks a
  // time window, and the three renderings must stay distinguishable: absent is
  // "we have not asked yet", not "booked".
  it('shows no badge when availability is unknown', async () => {
    await renderWithRouter(<RoomCard room={room()} href={{ to: '/' }} />)

    // Guards the two assertions below: they would also hold against an empty
    // document, so something has to prove the card actually rendered.
    expect(screen.getByText('Aurora')).toBeInTheDocument()
    expect(screen.queryByText('Free')).not.toBeInTheDocument()
    expect(screen.queryByText('Booked')).not.toBeInTheDocument()
  })

  it('shows Free when available is true', async () => {
    await renderWithRouter(<RoomCard room={room({ available: true })} href={{ to: '/' }} />)

    expect(screen.getByText('Free')).toBeInTheDocument()
    expect(screen.queryByText('Booked')).not.toBeInTheDocument()
  })

  it('shows Booked when available is false', async () => {
    await renderWithRouter(<RoomCard room={room({ available: false })} href={{ to: '/' }} />)

    expect(screen.getByText('Booked')).toBeInTheDocument()
    expect(screen.queryByText('Free')).not.toBeInTheDocument()
  })

  it('dims only the unavailable card', async () => {
    // The visual half of the same tri-state. `opacity-60` keyed off `=== false`
    // is what would go wrong if someone rewrote the check as `!room.available`:
    // every card would render dimmed before a window is chosen.
    const { container: unknown } = await renderWithRouter(
      <RoomCard room={room()} href={{ to: '/' }} />,
    )
    expect(unknown.querySelector('.opacity-60')).toBeNull()

    cleanup()

    const { container: booked } = await renderWithRouter(
      <RoomCard room={room({ available: false })} href={{ to: '/' }} />,
    )
    expect(booked.querySelector('.opacity-60')).not.toBeNull()
  })
})

describe('RoomCard link', () => {
  it('names the room, and only the room, as the link text', async () => {
    // The ::after overlay exists so the anchor covers the card without
    // containing it. If someone "simplifies" it by wrapping the card in the
    // Link, the accessible name becomes the name, the seat count and every
    // amenity read as one link — this is the assertion that catches that.
    await renderWithRouter(
      <RoomCard room={room()} href={{ to: '/rooms/$roomId', params: { roomId: ROOM_ID } }} />,
      { paths: ['/rooms/$roomId'] },
    )

    const link = screen.getByRole('link', { name: 'Aurora' })
    expect(link).toHaveAttribute('href', `/rooms/${ROOM_ID}`)
  })

  it('carries search params through to the resolved href', async () => {
    // Browse passes the chosen window down so the detail page opens on the same
    // day the user was looking at rather than resetting to today.
    await renderWithRouter(
      <RoomCard
        room={room()}
        href={{
          to: '/rooms/$roomId',
          params: { roomId: ROOM_ID },
          search: { date: '2026-06-15' },
        }}
      />,
      { paths: ['/rooms/$roomId'] },
    )

    expect(screen.getByRole('link', { name: 'Aurora' })).toHaveAttribute(
      'href',
      `/rooms/${ROOM_ID}?date=2026-06-15`,
    )
  })
})

describe('RoomCard amenities', () => {
  it('renders each amenity as a list item', async () => {
    await renderWithRouter(
      <RoomCard room={room({ amenities: ['tv', 'whiteboard'] })} href={{ to: '/' }} />,
    )

    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('TV')).toBeInTheDocument()
  })

  it('renders no list at all when there are none', async () => {
    // An empty <ul> is announced as "list, 0 items", which is noise.
    await renderWithRouter(<RoomCard room={room({ amenities: [] })} href={{ to: '/' }} />)

    expect(screen.getByText('Aurora')).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('shows the seat count', async () => {
    await renderWithRouter(<RoomCard room={room({ capacity: 12 })} href={{ to: '/' }} />)

    expect(screen.getByText('Seats 12')).toBeInTheDocument()
  })
})

describe('formatAmenity', () => {
  it.each([
    ['tv', 'TV'],
    ['videoconf', 'Video conf'],
    ['whiteboard', 'Whiteboard'],
    ['projector', 'Projector'],
  ])('renders %s as %s', (stored, displayed) => {
    expect(formatAmenity(stored)).toBe(displayed)
  })

  it('leaves an unknown amenity capitalised rather than dropping it', () => {
    // Amenities are free-form strings from the database, not an enum. A new one
    // added by an admin has to render as something.
    expect(formatAmenity('sauna')).toBe('Sauna')
    expect(formatAmenity('')).toBe('')
  })
})
