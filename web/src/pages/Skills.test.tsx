import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Skills from './Skills'
import { api } from '../api'
import type { SkillSource } from '../types'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    api: {
      skills: vi.fn(),
      skillSources: vi.fn(),
      addSkillSource: vi.fn(),
      updateSkillSource: vi.fn(),
      refreshSkillSource: vi.fn(),
      restoreSkillSource: vi.fn(),
      deleteSkillSource: vi.fn(),
    },
  }
})

const activeSource: SkillSource = {
  id: 30003,
  url: 'https://github.com/example/skills.git',
  ref: 'master',
  subdir: '',
  skill_filters: ['research'],
  status: 'active',
  version: 'abcdef1234567890',
}

const removedSource: SkillSource = {
  ...activeSource,
  id: 30002,
  ref: 'main',
  skill_filters: [],
  status: 'deleted',
  version: '',
}

function renderSkills() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><Skills /></QueryClientProvider>)
}

describe('Skills sources', () => {
  beforeEach(() => {
    vi.mocked(api.skills).mockReset()
    vi.mocked(api.skillSources).mockReset()
    vi.mocked(api.addSkillSource).mockReset()
    vi.mocked(api.updateSkillSource).mockReset()
    vi.mocked(api.refreshSkillSource).mockReset()
    vi.mocked(api.restoreSkillSource).mockReset()
    vi.mocked(api.deleteSkillSource).mockReset()
    vi.mocked(api.skills).mockResolvedValue({ skills: [] })
    vi.mocked(api.skillSources).mockResolvedValue({ sources: [activeSource, removedSource] })
    vi.mocked(api.restoreSkillSource).mockResolvedValue({ source: { ...removedSource, status: 'active' } })
  })

  it('keeps removed sources out of the active list and restores them from a separate section', async () => {
    renderSkills()
    await screen.findByText(activeSource.url)
    expect(screen.getByText('1', { selector: '.stats-row b' })).toBeInTheDocument()
    expect(screen.getAllByText(activeSource.url)).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: /Removed sources/ }))
    expect(screen.getAllByText(activeSource.url)).toHaveLength(2)
    fireEvent.click(screen.getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(api.restoreSkillSource).toHaveBeenCalled())
    expect(vi.mocked(api.restoreSkillSource).mock.calls[0][0]).toBe(removedSource.id)
  })

  it('prefills and submits all editable source fields', async () => {
    const updated = { ...activeSource, ref: 'release', subdir: 'skills', skill_filters: ['release-notes', 'research'] }
    vi.mocked(api.updateSkillSource).mockResolvedValue({ source: updated })
    renderSkills()

    fireEvent.click(await screen.findByRole('button', { name: `Edit ${activeSource.url}` }))
    expect(screen.getByLabelText('Repository URL')).toHaveValue(activeSource.url)
    expect(screen.getByLabelText('Git ref')).toHaveValue('master')
    expect(screen.getByLabelText(/Skill filters/)).toHaveValue('research')

    fireEvent.change(screen.getByLabelText('Git ref'), { target: { value: 'release' } })
    fireEvent.change(screen.getByLabelText('Subdirectory'), { target: { value: 'skills' } })
    fireEvent.change(screen.getByLabelText(/Skill filters/), { target: { value: 'research, release-notes' } })
    fireEvent.click(screen.getByRole('button', { name: /Save and sync/ }))

    await waitFor(() => expect(api.updateSkillSource).toHaveBeenCalledWith(activeSource.id, {
      url: activeSource.url,
      ref: 'release',
      subdir: 'skills',
      skill_filters: ['research', 'release-notes'],
    }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
