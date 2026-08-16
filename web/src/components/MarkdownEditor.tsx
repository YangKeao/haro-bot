import { useEffect, useRef, useState } from 'react'
import { EditorContent, useEditor, type Editor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Markdown } from '@tiptap/markdown'
import { Placeholder } from '@tiptap/extension-placeholder'
import { TaskItem } from '@tiptap/extension-task-item'
import { TaskList } from '@tiptap/extension-task-list'
import { TableKit } from '@tiptap/extension-table'
import {
  Bold, Braces, Code2, Heading1, Heading2, Italic, Link2, List, ListChecks,
  ListOrdered, MessageSquareQuote, Minus, Redo2, Strikethrough, Table2, Undo2,
} from 'lucide-react'

type MarkdownEditorProps = {
  value: string
  onChange: (value: string) => void
  variant?: 'composer' | 'document'
  placeholder?: string
  disabled?: boolean
  ariaLabel?: string
  onSubmit?: () => void
}

export default function MarkdownEditor({
  value, onChange, variant = 'document', placeholder = 'Write in Markdown…', disabled = false,
  ariaLabel = 'Markdown editor', onSubmit,
}: MarkdownEditorProps) {
  const [sourceMode, setSourceMode] = useState(false)
  const valueRef = useRef(value)
  const submitRef = useRef(onSubmit)
  valueRef.current = value
  submitRef.current = onSubmit

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: { openOnClick: false, autolink: true } }),
      TaskList,
      TaskItem.configure({ nested: true }),
      TableKit.configure({ table: { resizable: false } }),
      Markdown.configure({ markedOptions: { gfm: true } }),
      Placeholder.configure({ placeholder }),
    ],
    content: value,
    contentType: 'markdown',
    editable: !disabled,
    immediatelyRender: false,
    editorProps: {
      attributes: { 'aria-label': ariaLabel, class: 'markdown-editor-content' },
      handleKeyDown: (_view, event) => {
        if (variant === 'composer' && event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
          event.preventDefault()
          submitRef.current?.()
          return true
        }
        return false
      },
    },
    onUpdate: ({ editor: activeEditor }) => {
			if (activeEditor.isFocused) onChange(activeEditor.getMarkdown())
		},
  }, [variant])

  useEffect(() => {
    if (!editor || sourceMode) return
    if (editor.getMarkdown() !== value) editor.commands.setContent(value, { contentType: 'markdown', emitUpdate: false })
  }, [editor, sourceMode, value])
  useEffect(() => { editor?.setEditable(!disabled) }, [disabled, editor])

  const toggleSource = () => {
    if (sourceMode && editor) editor.commands.setContent(valueRef.current, { contentType: 'markdown', emitUpdate: false })
    setSourceMode(current => !current)
  }

  return <div className={`markdown-editor ${variant} ${disabled ? 'disabled' : ''}`}>
    <EditorToolbar editor={editor} documentMode={variant === 'document'} sourceMode={sourceMode} onToggleSource={toggleSource} />
    {sourceMode && variant === 'document'
      ? <textarea className="markdown-source" value={value} onChange={event => onChange(event.target.value)} disabled={disabled} aria-label={`${ariaLabel} Markdown source`} spellCheck={false} />
      : <EditorContent editor={editor} />}
  </div>
}

function EditorToolbar({ editor, documentMode, sourceMode, onToggleSource }: {
  editor: Editor | null
  documentMode: boolean
  sourceMode: boolean
  onToggleSource: () => void
}) {
  const setLink = () => {
    if (!editor) return
    const previous = editor.getAttributes('link').href as string | undefined
    const href = window.prompt('Link URL', previous || 'https://')
    if (href === null) return
    if (!href.trim()) editor.chain().focus().extendMarkRange('link').unsetLink().run()
    else editor.chain().focus().extendMarkRange('link').setLink({ href: href.trim() }).run()
  }
  return <div className="markdown-toolbar" role="toolbar" aria-label="Markdown formatting">
    {!sourceMode && <>
      {documentMode && <>
        <ToolButton label="Undo" disabled={!editor?.can().undo()} onClick={() => editor?.chain().focus().undo().run()}><Undo2 /></ToolButton>
        <ToolButton label="Redo" disabled={!editor?.can().redo()} onClick={() => editor?.chain().focus().redo().run()}><Redo2 /></ToolButton>
        <span className="toolbar-divider" />
        <ToolButton label="Heading 1" active={editor?.isActive('heading', { level: 1 })} onClick={() => editor?.chain().focus().toggleHeading({ level: 1 }).run()}><Heading1 /></ToolButton>
        <ToolButton label="Heading 2" active={editor?.isActive('heading', { level: 2 })} onClick={() => editor?.chain().focus().toggleHeading({ level: 2 }).run()}><Heading2 /></ToolButton>
      </>}
      <ToolButton label="Bold" active={editor?.isActive('bold')} onClick={() => editor?.chain().focus().toggleBold().run()}><Bold /></ToolButton>
      <ToolButton label="Italic" active={editor?.isActive('italic')} onClick={() => editor?.chain().focus().toggleItalic().run()}><Italic /></ToolButton>
      {documentMode && <ToolButton label="Strikethrough" active={editor?.isActive('strike')} onClick={() => editor?.chain().focus().toggleStrike().run()}><Strikethrough /></ToolButton>}
      <ToolButton label="Inline code" active={editor?.isActive('code')} onClick={() => editor?.chain().focus().toggleCode().run()}><Braces /></ToolButton>
      <ToolButton label="Link" active={editor?.isActive('link')} onClick={setLink}><Link2 /></ToolButton>
      <span className="toolbar-divider" />
      <ToolButton label="Bullet list" active={editor?.isActive('bulletList')} onClick={() => editor?.chain().focus().toggleBulletList().run()}><List /></ToolButton>
      <ToolButton label="Numbered list" active={editor?.isActive('orderedList')} onClick={() => editor?.chain().focus().toggleOrderedList().run()}><ListOrdered /></ToolButton>
      <ToolButton label="Task list" active={editor?.isActive('taskList')} onClick={() => editor?.chain().focus().toggleTaskList().run()}><ListChecks /></ToolButton>
      <ToolButton label="Quote" active={editor?.isActive('blockquote')} onClick={() => editor?.chain().focus().toggleBlockquote().run()}><MessageSquareQuote /></ToolButton>
      <ToolButton label="Code block" active={editor?.isActive('codeBlock')} onClick={() => editor?.chain().focus().toggleCodeBlock().run()}><Code2 /></ToolButton>
      {documentMode && <>
        <ToolButton label="Horizontal rule" onClick={() => editor?.chain().focus().setHorizontalRule().run()}><Minus /></ToolButton>
        <ToolButton label="Insert table" onClick={() => editor?.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()}><Table2 /></ToolButton>
      </>}
    </>}
    {documentMode && <button type="button" className={`source-toggle ${sourceMode ? 'active' : ''}`} onClick={onToggleSource}>{sourceMode ? 'Visual' : 'Markdown'}</button>}
  </div>
}

function ToolButton({ label, active, disabled, onClick, children }: {
  label: string
  active?: boolean
  disabled?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return <button type="button" className={active ? 'active' : ''} aria-label={label} title={label} disabled={disabled} onClick={onClick}>{children}</button>
}
