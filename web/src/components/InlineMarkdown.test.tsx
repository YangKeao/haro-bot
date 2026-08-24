import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import InlineMarkdown from './InlineMarkdown'

describe('InlineMarkdown', () => {
  it('renders CommonMark escapes and formatting without block or link elements', () => {
    const title = 'Investigate tidb\\_enable\\_check\\_constraint with **care** and [docs](https://example.com)'
    const { container } = render(<div data-testid="title"><InlineMarkdown>{title}</InlineMarkdown></div>)

    expect(screen.getByTestId('title')).toHaveTextContent('Investigate tidb_enable_check_constraint with care and docs')
    expect(screen.getByTestId('title')).not.toHaveTextContent('\\')
    expect(screen.getByText('care').tagName).toBe('STRONG')
    expect(container.querySelector('p')).toBeNull()
    expect(container.querySelector('a')).toBeNull()
  })
})
