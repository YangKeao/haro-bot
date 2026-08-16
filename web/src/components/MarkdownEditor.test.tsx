import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import MarkdownEditor from './MarkdownEditor'

function DocumentHarness() {
  const [value, setValue] = useState('## Heading\n\n- one\n- two')
  return <MarkdownEditor value={value} onChange={setValue} ariaLabel="Instructions" />
}

describe('MarkdownEditor', () => {
  it('offers Markdown source mode for document editors', async () => {
    render(<DocumentHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'Markdown' }))
    const source = screen.getByLabelText('Instructions Markdown source')
    expect(source).toHaveValue('## Heading\n\n- one\n- two')
    fireEvent.change(source, { target: { value: '**updated**' } })
    expect(source).toHaveValue('**updated**')
    fireEvent.click(screen.getByRole('button', { name: 'Visual' }))
    expect(await screen.findByText('updated')).toBeInTheDocument()
  })

  it('submits a composer with Cmd/Ctrl+Enter without hijacking plain Enter', () => {
    const submit = vi.fn()
    render(<MarkdownEditor variant="composer" value="hello" onChange={() => undefined} onSubmit={submit} ariaLabel="Message" />)
    const editor = screen.getByLabelText('Message')
    fireEvent.keyDown(editor, { key: 'Enter' })
    expect(submit).not.toHaveBeenCalled()
    fireEvent.keyDown(editor, { key: 'Enter', ctrlKey: true })
    expect(submit).toHaveBeenCalledTimes(1)
  })
})
