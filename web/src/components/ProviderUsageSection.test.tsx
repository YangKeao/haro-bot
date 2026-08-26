import { act, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, APIError } from '../api'
import ProviderUsageSection from './ProviderUsageSection'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return { ...actual, api: { providerUsage: vi.fn() } }
})

const usage = {
  fetched_at: '2026-08-26T12:00:00Z',
  limits: [{
    id: 'codex', name: 'Codex', allowed: true, limit_reached: false,
    windows: [
      { kind: 'primary' as const, used_percent: 42.5, window_seconds: 18_000, resets_at: '2026-08-26T17:00:00Z' },
      { kind: 'secondary' as const, used_percent: 12, window_seconds: 604_800, resets_at: '2026-09-02T12:00:00Z' },
    ],
  }],
}

function renderUsage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><ProviderUsageSection providerID={7} /></QueryClientProvider>)
}

describe('ProviderUsageSection', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(api.providerUsage).mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders every window and refreshes once per minute', async () => {
    vi.mocked(api.providerUsage).mockResolvedValue(usage)
    renderUsage()

    await vi.waitFor(() => expect(screen.getByText('5-hour limit')).toBeInTheDocument())
    expect(screen.getByText('7-day limit')).toBeInTheDocument()
    expect(screen.getByText('42.5% used')).toBeInTheDocument()
    expect(screen.getAllByRole('progressbar')).toHaveLength(2)
    expect(screen.getAllByText(/Resets /)[0].closest('time')).toHaveAttribute('datetime', '2026-08-26T17:00:00Z')
    expect(api.providerUsage).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    await vi.waitFor(() => expect(api.providerUsage).toHaveBeenCalledTimes(2))
    fireEvent.click(screen.getByRole('button', { name: 'Refresh usage' }))
    await vi.waitFor(() => expect(api.providerUsage).toHaveBeenCalledTimes(3))
  })

  it('shows a compatibility state for unsupported providers', async () => {
    vi.mocked(api.providerUsage).mockRejectedValue(new APIError(501, 'provider_usage_unsupported', 'Not supported'))
    renderUsage()
    await vi.waitFor(() => expect(screen.getByText('Usage is not available')).toBeInTheDocument())
    expect(screen.getByText('This provider does not expose compatible usage data.')).toBeInTheDocument()
  })
})
