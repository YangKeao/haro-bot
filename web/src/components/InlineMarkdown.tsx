import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const inlineElements = ['p', 'strong', 'em', 'del', 'code']

export default function InlineMarkdown({ children }: { children: string }) {
  return <ReactMarkdown
    remarkPlugins={[remarkGfm]}
    allowedElements={inlineElements}
    unwrapDisallowed
    components={{ p: ({ children: content }) => <>{content}</> }}
  >{children}</ReactMarkdown>
}
