import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import TraceSidebar from './TraceSidebar'
import type { TraceStep } from '../types'

describe('TraceSidebar', () => {
  it('renders reasoning and hosted tools in their recorded order', () => {
    const steps: TraceStep[] = [
      { id: 'reasoning-1', kind: 'reasoning', status: 'completed', content: 'Plan the search' },
      {
        id: 'search-1',
        kind: 'tool',
        tool_kind: 'hosted',
        name: 'web_search',
        status: 'completed',
        detail: { action: { type: 'search', query: 'latest release', sources: [{ url: 'https://example.com/source' }] } },
      },
      { id: 'reasoning-2', kind: 'reasoning', status: 'completed', content: 'Summarize the result' },
    ]

    render(<TraceSidebar steps={steps} running={false} onClose={() => undefined} />)

    const firstReasoning = screen.getByText('Plan the search')
    const search = screen.getByText('Web search')
    const secondReasoning = screen.getByText('Summarize the result')
    expect(firstReasoning.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(search.compareDocumentPosition(secondReasoning) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText('latest release')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'https://example.com/source' })).toHaveAttribute('href', 'https://example.com/source')
  })
})
