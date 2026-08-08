import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DatePicker } from './DatePicker'

// June 2026 grid fact used throughout: June 1 is a Monday, so the six-week grid
// runs May 31 -> Jul 11. Day numbers 12..30 appear once (June only), which is
// what lets these tests target a day by its number without locale-dependent
// label matching. 12 and 20 are both unique to June here.

describe('DatePicker', () => {
  it('shows a placeholder and stays closed until opened', () => {
    render(<DatePicker label="Date" value={undefined} onChange={() => {}} />)

    // Positive fact so the negative below cannot pass against an empty render.
    expect(screen.getByRole('button', { name: /pick a date/i })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens onto the selected month and reports the day clicked as YYYY-MM-DD', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<DatePicker label="Date" value="2026-06-15" onChange={onChange} />)

    await user.click(screen.getByRole('button'))

    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.click(screen.getByText('20'))

    expect(onChange).toHaveBeenCalledWith('2026-06-20')
  })

  it('disables days before the min date and leaves the rest pickable', async () => {
    const user = userEvent.setup()
    render(<DatePicker label="Date" value="2026-06-15" min="2026-06-15" onChange={vi.fn()} />)

    await user.click(screen.getByRole('button'))

    expect(screen.getByText('12')).toBeDisabled() // June 12, before the min
    expect(screen.getByText('20')).toBeEnabled() // June 20, on/after it
  })
})
