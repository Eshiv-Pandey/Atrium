import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { ApiError, errorMessage } from '@/api/client'
import { useCreateRoom, useDeleteRoom, useRooms, useUpdateRoom } from '@/api/hooks'
import type { Room } from '@/api/schemas'
import { formatAmenity } from '@/components/RoomCard'
import { notify } from '@/components/Toast'
import { Button } from '@/components/ui/Button'
import { Badge, Card, EmptyState, ErrorState, Skeleton } from '@/components/ui/Card'
import { Dialog } from '@/components/ui/Dialog'
import { Field } from '@/components/ui/Field'

export const Route = createFileRoute('/admin/rooms')({
  component: AdminRooms,
})

const AMENITIES = ['quiet', 'whiteboard', 'tv', 'videoconf', 'casual']

function AdminRooms() {
  const rooms = useRooms()

  // One piece of state for the dialog, not a boolean plus a nullable room.
  // With two, "creating" and "editing something" are independently
  // representable, and the pair (true, someRoom) has no meaning — the dialog
  // would have to pick a winner. A single tagged value makes the impossible
  // state unrepresentable.
  const [target, setTarget] = useState<{ room: Room | null } | null>(null)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="font-medium">Rooms</h2>
        <Button onClick={() => setTarget({ room: null })}>Add room</Button>
      </div>

      <div aria-live="polite" aria-busy={rooms.isLoading}>
        {rooms.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : rooms.isError ? (
          <ErrorState
            title="Could not load rooms"
            description={errorMessage(rooms.error)}
            onRetry={() => void rooms.refetch()}
          />
        ) : (rooms.data?.length ?? 0) === 0 ? (
          <EmptyState
            title="No rooms yet"
            description="Add the first bookable space."
            action={<Button onClick={() => setTarget({ room: null })}>Add room</Button>}
          />
        ) : (
          <Card className="overflow-hidden">
            {/*
              A real table, not a grid of divs. Row and column headers are what
              let a screen reader announce "Capacity, 6" while moving across a
              row; a div grid announces a stream of unlabelled numbers.
            */}
            <table className="w-full text-sm">
              <caption className="sr-only">
                All bookable rooms, with capacity and amenities.
              </caption>
              <thead className="border-b border-border bg-muted/50 text-left">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">Name</th>
                  <th scope="col" className="px-4 py-3 font-medium">Seats</th>
                  <th scope="col" className="px-4 py-3 font-medium">Amenities</th>
                  <th scope="col" className="px-4 py-3 text-right font-medium">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rooms.data?.map((room) => (
                  <RoomRow
                    key={room.id}
                    room={room}
                    onEdit={() => setTarget({ room })}
                  />
                ))}
              </tbody>
            </table>
          </Card>
        )}
      </div>

      {/*
        Mounted only while open, and keyed by the room it targets. Both matter:
        keying resets the form fields when switching from one room to another
        without an effect that copies props into state, and unmounting on close
        means the next open starts clean rather than showing the last room's
        values for a frame.
      */}
      {target ? (
        <RoomDialogForm
          key={target.room?.id ?? 'new'}
          open
          onClose={() => setTarget(null)}
          room={target.room}
        />
      ) : null}
    </div>
  )
}

function RoomRow({ room, onEdit }: { room: Room; onEdit: () => void }) {
  const remove = useDeleteRoom()

  return (
    <tr className="border-b border-border last:border-0">
      <th scope="row" className="px-4 py-3 text-left font-medium">
        {room.name}
      </th>
      <td className="px-4 py-3 tabular-nums">{room.capacity}</td>
      <td className="px-4 py-3">
        {room.amenities.length > 0 ? (
          <ul className="flex flex-wrap gap-1">
            {room.amenities.map((a) => (
              <li key={a}>
                <Badge>{formatAmenity(a)}</Badge>
              </li>
            ))}
          </ul>
        ) : (
          <span className="text-muted-fg">—</span>
        )}
      </td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={onEdit}>
            Edit
          </Button>
          <Button
            size="sm"
            variant="ghost"
            loading={remove.isPending}
            onClick={() => {
              if (!window.confirm(`Delete ${room.name}?`)) return
              remove.mutate(room.id, {
                onSuccess: () => notify.success(`${room.name} deleted.`),
                onError: (err) => {
                  // The server refuses to delete a room with future bookings
                  // and says so. Surfacing its message rather than a generic
                  // failure is the difference between "try again" and "cancel
                  // the three bookings first".
                  if (err instanceof ApiError && err.isConflict) {
                    notify.error(err.message)
                    return
                  }
                  notify.error(errorMessage(err))
                },
              })
            }}
          >
            Delete
          </Button>
        </div>
      </td>
    </tr>
  )
}

/**
 * RoomDialogForm creates or edits a room.
 *
 * One component for both because the fields and the validation are identical
 * and only the verb differs — two near-copies would drift the moment a field
 * is added to one of them.
 */
function RoomDialogForm({
  open,
  onClose,
  room,
}: {
  open: boolean
  onClose: () => void
  room: Room | null
}) {
  const create = useCreateRoom()
  const update = useUpdateRoom()
  const mutation = room ? update : create

  const [name, setName] = useState(room?.name ?? '')
  const [capacity, setCapacity] = useState(room?.capacity ?? 4)
  const [amenities, setAmenities] = useState<string[]>(room?.amenities ?? [])

  const nameError = name.trim() === '' ? 'Give the room a name.' : undefined
  const capacityError = capacity < 1 ? 'Capacity must be at least 1.' : undefined
  const invalid = Boolean(nameError || capacityError)

  const submit = () => {
    if (invalid) return

    const done = {
      onSuccess: () => {
        notify.success(room ? 'Room updated.' : 'Room created.')
        onClose()
      },
      onError: (err: unknown) => notify.error(errorMessage(err)),
    }

    if (room) {
      update.mutate({ id: room.id, name: name.trim(), capacity, amenities }, done)
    } else {
      create.mutate({ name: name.trim(), capacity, amenities }, done)
    }
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={room ? `Edit ${room.name}` : 'Add room'}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button onClick={submit} loading={mutation.isPending} disabled={invalid}>
            {room ? 'Save changes' : 'Create room'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field
          label="Name"
          value={name}
          error={name !== '' ? nameError : undefined}
          onChange={(e) => setName(e.target.value)}
        />
        <Field
          label="Capacity"
          type="number"
          min={1}
          inputMode="numeric"
          value={capacity}
          error={capacityError}
          onChange={(e) => setCapacity(Number(e.target.value) || 0)}
        />

        <fieldset>
          <legend className="text-sm font-medium">Amenities</legend>
          <div className="mt-2 flex flex-wrap gap-2">
            {AMENITIES.map((amenity) => {
              const active = amenities.includes(amenity)
              return (
                <button
                  key={amenity}
                  type="button"
                  aria-pressed={active}
                  onClick={() =>
                    setAmenities((prev) =>
                      prev.includes(amenity)
                        ? prev.filter((a) => a !== amenity)
                        : [...prev, amenity],
                    )
                  }
                  className={
                    active
                      ? 'rounded-full border border-primary bg-primary/10 px-3 py-1 text-sm text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                      : 'rounded-full border border-border px-3 py-1 text-sm text-muted-fg hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                  }
                >
                  {formatAmenity(amenity)}
                </button>
              )
            })}
          </div>
        </fieldset>
      </div>
    </Dialog>
  )
}
