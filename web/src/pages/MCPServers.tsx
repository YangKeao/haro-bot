import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cable, CircleAlert, Pencil, Plus, Save, Trash2, X } from 'lucide-react'
import { api, APIError } from '../api'
import type { MCPServer, MCPServerInput } from '../types'

const blank: MCPServerInput = {
  name: '', description: '', transport: 'http', command: '', args: [], url: '', oauth_enabled: false,
  oauth_authorization_endpoint: '', oauth_token_endpoint: '', oauth_registration_endpoint: '', oauth_client_id: '', oauth_client_secret: '', oauth_scopes: '', enabled: true,
}

function fromServer(server: MCPServer): MCPServerInput {
  return { ...blank, ...server, args: server.args || [], oauth_client_secret: '' }
}

export default function MCPServers({ embedded = false }: { embedded?: boolean }) {
  const client = useQueryClient()
  const servers = useQuery({ queryKey: ['mcp-servers'], queryFn: () => api.mcpServers(true) })
  const [editing, setEditing] = useState<number | 'new'>()
  const [form, setForm] = useState<MCPServerInput>(blank)
  const [argsText, setArgsText] = useState('')
  const selected = editing === 'new' ? undefined : servers.data?.servers.find(item => item.id === editing)
  useEffect(() => {
    const next = selected ? fromServer(selected) : { ...blank }
    setForm(next); setArgsText((next.args || []).join('\n'))
  }, [editing, selected])
  const save = useMutation({
    mutationFn: () => {
      const input = { ...form, args: argsText.split('\n').map(item => item.trim()).filter(Boolean), oauth_client_secret: form.oauth_client_secret || undefined }
      return editing === 'new' ? api.createMCPServer(input) : api.updateMCPServer(editing!, input)
    },
    onSuccess: () => { client.invalidateQueries({ queryKey: ['mcp-servers'] }); setEditing(undefined) },
  })
  const remove = useMutation({ mutationFn: (id: number) => api.deleteMCPServer(id), onSuccess: () => { client.invalidateQueries({ queryKey: ['mcp-servers'] }); setEditing(undefined) } })
  const submit = (event: FormEvent) => { event.preventDefault(); save.mutate() }
  const Heading = embedded ? 'h2' : 'h1'
  return <>
    <header className="page-header"><div><div className="eyebrow">Model Context Protocol</div><Heading>MCP servers</Heading><p className="muted">Connect Streamable HTTP services or run stdio servers inside an Agent Sandbox. Tool schemas are deferred until the agent searches for them.</p></div><button className="button primary" onClick={() => setEditing('new')}><Plus size={16} /> New server</button></header>
    {servers.isLoading ? <div className="skeleton tall" /> : servers.data?.servers.length ? <div className="provider-grid">{servers.data.servers.map(server => <article className="provider-card" key={server.id}>
      <div className="provider-card-icon"><Cable /></div><div className="provider-card-copy"><h3>{server.name}</h3><p>{server.description || `${server.transport === 'http' ? server.url : server.command}`}</p><div className="provider-meta"><span>{server.transport}</span><span>{server.enabled ? 'Enabled' : 'Disabled'}</span>{server.oauth_enabled && <span>OAuth</span>}</div>{server.last_error && <div className="form-error"><CircleAlert size={14} /> {server.last_error}</div>}</div>
      <button className="icon-button" aria-label={`Edit ${server.name}`} onClick={() => setEditing(server.id)}><Pencil size={16} /></button>
    </article>)}</div> : <section className="empty-state card"><Cable /><h2>No MCP servers</h2><p>Add an HTTP service, or a stdio command that will run inside assigned agents' Sandboxes.</p><button className="button primary" onClick={() => setEditing('new')}><Plus size={16} /> Add MCP server</button></section>}
    {editing && <form className="form-section settings-section" onSubmit={submit}>
      <div className="section-heading"><div><h3>{editing === 'new' ? 'New MCP server' : `Edit ${selected?.name || 'server'}`}</h3><p>Credentials are configured per Agent after assignment.</p></div><button type="button" className="icon-button" onClick={() => setEditing(undefined)}><X /></button></div>
      <div className="form-grid"><label><span>Name</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required maxLength={128} /></label><label><span>Transport</span><select value={form.transport} onChange={e => setForm({ ...form, transport: e.target.value as 'http' | 'stdio' })}><option value="http">Streamable HTTP</option><option value="stdio">stdio in Sandbox</option></select></label></div>
      <label><span>Description</span><textarea rows={2} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
      {form.transport === 'http' ? <label><span>MCP endpoint URL</span><input type="url" value={form.url} onChange={e => setForm({ ...form, url: e.target.value })} placeholder="https://example.com/mcp" required /></label> : <><label><span>Command</span><input value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} placeholder="npx" required /></label><label><span>Arguments</span><textarea rows={4} value={argsText} onChange={e => setArgsText(e.target.value)} placeholder={'One argument per line\n-y\n@company/mcp-server'} /></label><p className="field-note">The command starts inside each assigned Agent's Sandbox and persists for that chat session.</p></>}
      {form.transport === 'http' && <div className="form-section nested-section"><label className="toggle-row"><input type="checkbox" checked={form.oauth_enabled} onChange={e => setForm({ ...form, oauth_enabled: e.target.checked })} /><span><b>OAuth 2.1</b><small>Authorization Code with PKCE; tokens are encrypted per Agent.</small></span></label>{form.oauth_enabled && <><div className="form-grid"><label><span>Authorization endpoint</span><input type="url" value={form.oauth_authorization_endpoint} onChange={e => setForm({ ...form, oauth_authorization_endpoint: e.target.value })} placeholder="Discovered automatically" /></label><label><span>Token endpoint</span><input type="url" value={form.oauth_token_endpoint} onChange={e => setForm({ ...form, oauth_token_endpoint: e.target.value })} placeholder="Discovered automatically" /></label></div><label><span>Dynamic registration endpoint</span><input type="url" value={form.oauth_registration_endpoint} onChange={e => setForm({ ...form, oauth_registration_endpoint: e.target.value })} placeholder="Discovered automatically" /></label><div className="form-grid"><label><span>Client ID</span><input value={form.oauth_client_id} onChange={e => setForm({ ...form, oauth_client_id: e.target.value })} placeholder="Dynamic registration if blank" /></label><label><span>Client secret</span><input type="password" value={form.oauth_client_secret || ''} onChange={e => setForm({ ...form, oauth_client_secret: e.target.value })} placeholder={selected?.oauth_client_secret_set ? 'Keep existing secret' : 'Optional'} /></label></div><label><span>Scopes</span><input value={form.oauth_scopes} onChange={e => setForm({ ...form, oauth_scopes: e.target.value })} placeholder="Discovered from protected resource metadata" /></label></>}</div>}
      <label className="toggle-row"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })} /><span><b>Enabled</b><small>Disabled servers remain configured but are unavailable to agents.</small></span></label>
      {save.error && <div className="form-error">{save.error instanceof APIError ? save.error.message : 'Could not save MCP server.'}</div>}
      <div className="test-row">{editing !== 'new' && <button type="button" className="button danger" disabled={remove.isPending} onClick={() => { if (window.confirm(`Delete ${selected?.name}? Agent assignments and credentials will also be removed.`)) remove.mutate(editing!) }}><Trash2 size={16} /> Delete</button>}<button className="button primary" disabled={save.isPending || !form.name.trim()}><Save size={16} /> Save server</button></div>
    </form>}
  </>
}
