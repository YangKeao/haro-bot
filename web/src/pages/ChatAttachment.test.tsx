import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { Attachment } from '../types'
import { AttachmentLink } from './Chat'

afterEach(cleanup)

describe('AttachmentLink', () => {
  it('renders a bounded image preview with a separate download action', () => {
    render(<AttachmentLink attachment={attachment('image-1', 'result.png', 'image/png')} />)

    expect(screen.getByRole('img', { name: 'result.png' })).toHaveAttribute('src', '/api/v1/attachments/image-1')
    expect(screen.getByRole('link', { name: 'Open result.png' })).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: 'Download result.png' })).toHaveAttribute('href', '/api/v1/attachments/image-1?download=1')
  })

  it('renders arbitrary files as attachment cards with download actions', () => {
    render(<AttachmentLink attachment={attachment('file-1', 'data.zip', 'application/zip')} />)

    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('data.zip')).toBeInTheDocument()
    expect(screen.getByText(/application\/zip/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download data.zip' })).toHaveAttribute('href', '/api/v1/attachments/file-1?download=1')
  })
})

function attachment(id: string, name: string, mimeType: string): Attachment {
  return { id, session_id: 1, name, mime_type: mimeType, size_bytes: 4096, created_at: new Date(0).toISOString() }
}
