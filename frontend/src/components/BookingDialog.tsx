import { useEffect, useState } from 'react'
import { ApiError, errorMessage } from '@/api/client'
import { useCreateBooking } from '@/api/hooks'
import type { Room } from '@/api/schemas'
import { notify } from '@/components/Toast'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { Field } from '@/components/ui/Field'
import { formatDuration, formatRange, toWire } from '@/lib/time'
import { newIdempotencyKey } from '@/lib/utils'

/**
 * BookingDialog confirms a selected range and creates the booking.
 *
 * The dialog is a separate step rather than a click-to-book timeline on
 * purpose. Booking is not reversible without a second action, the range is
 * easy to misdrag by one slot, and attendee count has to come from somewhere.
 * A confirmation step that restates the range in words is the cheapest way to
 * catch "I meant 2pm, not 2:15".
 */
export function BookingDialog({
  open,
  room,
  selection,
  onClose,
  onBooked,
}: {
  open: boolean
  room: Room
  selection: { start: Date; end: Date } | null
  onClose: () => void
  onBooked: () => void
}) {
  const create = useCreateBooking()
  const [attendees, setAttendees] = useState(1)
  const [idempotencyKey, setIdempotencyKey] = useState(newIdempotencyKey)

  // A fresh key per opening, not per submit and not per component instance.
  //
  // Per submit would defeat the purpose — two clicks would carry two keys and
  // make two bookings. Per instance would be worse in the other direction:
  // after a successful booking the same key would be reused for the next one,
  // and the server would replay the first booking instead of making a second.
  // Opening the dialog is exactly the boundary of one booking attempt.
  useEffect(() => {
    if (open) {
      setIdempotencyKey(newIdempotencyKey())
      setAttendees(1)
      create.reset()
    }
    // create.reset is stable; depending on `create` would re-run this on every
    // mutation state change and reset the form mid-submit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  if (!selection) return null

  const overCapacity = attendees > room.capacity

  const submit = () => {
    if (overCapacity) return

    create.mutate(
      {
        roomId: room.id,
        startTime: toWire(selection.start),
        endTime: toWire(selection.end),
        attendeeCount: attendees,
        idempotencyKey,
      },
      {
        onSuccess: () => {
          notify.success(`Booked ${room.name} · ${formatRange(selection.start, selection.end)}`)
          onBooked()
          onClose()
        },
        // The error is rendered inline rather than toasted: the user is looking
        // at this dialog, and a 409 needs the surrounding context ("this slot,
        // this room") to make sense. onError is left to the hook, which
        // refetches availability so the timeline behind the dialog is already
        // correct by the time it is dismissed.
      },
    )
  }

  const conflict = create.error instanceof ApiError && create.error.isConflict

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`Book ${room.name}`}
      description={`${formatRange(selection.start, selection.end)} · ${formatDuration(selection.start, selection.end)}`}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={create.isPending}>
            Cancel
          </Button>
          <Button onClick={submit} loading={create.isPending} disabled={overCapacity}>
            Confirm booking
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field
          label="Attendees"
          type="number"
          min={1}
          max={room.capacity}
          inputMode="numeric"
          value={attendees}
          error={overCapacity ? `This room seats ${room.capacity}.` : undefined}
          hint={`Seats up to ${room.capacity}.`}
          onChange={(e) => setAttendees(Number(e.target.value) || 1)}
        />

        {create.isError ? (
          <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {conflict
              ? 'Someone booked this slot while you were choosing. The timeline behind this dialog has been refreshed — pick another time.'
              : errorMessage(create.error)}
          </p>
        ) : null}
      </div>
    </Dialog>
  )
}
