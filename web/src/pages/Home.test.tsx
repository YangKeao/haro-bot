import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Home from './Home'
import { api } from '../api'

vi.mock('../api', () => ({ api: { agents: vi.fn(), recentSessions: vi.fn() } }))

describe('Home', () => {
  beforeEach(() => {
    vi.mocked(api.agents).mockReset()
    vi.mocked(api.recentSessions).mockReset()
    vi.mocked(api.recentSessions).mockResolvedValue({ sessions: [] })
  })

  it('shows onboarding when the workspace has no agents', async () => {
    vi.mocked(api.agents).mockResolvedValue({ agents: [] })
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter><Home /></MemoryRouter></QueryClientProvider>)
    expect(await screen.findByText('Build your first agent')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /create agent/i })).toHaveAttribute('href', '/agents/new')
  })

  it('renders configured agents with model and skill count', async () => {
    vi.mocked(api.agents).mockResolvedValue({ agents: [{
		id: 7, provider_id: 2, sandbox_id: null, provider_name: 'Example', name: 'Researcher', description: 'Finds evidence', icon: 'search', color: '#2563eb', avatar_mode: 'icon',
		instructions: '', model: 'vision-model', reasoning_effort_override: null, context_window_override: null,
		resolved_context_window: 128000, auto_compact_token_limit_override: null, resolved_auto_compact_token_limit: 115200,
		effective_context_window_percent: 95, skill_names: ['search'],
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }] })
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter><Home /></MemoryRouter></QueryClientProvider>)
    expect(await screen.findByText('Researcher')).toBeInTheDocument()
    expect(screen.getByText('vision-model')).toBeInTheDocument()
    expect(screen.getByText('1 skills')).toBeInTheDocument()
  })

  it('puts recent conversations ahead of the agent grid', async () => {
    vi.mocked(api.agents).mockResolvedValue({ agents: [] })
    vi.mocked(api.recentSessions).mockResolvedValue({ sessions: [{
      id: 42, agent_id: 7, title: 'Architecture review', created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      agent: { id: 7, name: 'Researcher', icon: 'search', color: '#557d78', avatar_mode: 'icon' },
    }] })
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter><Home /></MemoryRouter></QueryClientProvider>)
    const recent = await screen.findByRole('link', { name: /Architecture review/ })
    expect(recent).toHaveAttribute('href', '/agents/7/sessions/42')
    const recentSection = recent.closest('.recent-section')
    expect(recentSection).not.toBeNull()
    expect(recentSection?.nextElementSibling).toHaveClass('agents-section')
  })
})
