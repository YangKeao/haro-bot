import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Box, Home, LogOut, Menu, PanelLeft, Plus, Settings, Sparkles, X } from 'lucide-react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import * as Dialog from '@radix-ui/react-dialog'
import { api } from '../api'
import type { AgentProfile } from '../types'
import { AppRoutes } from '../App'
import { AgentAvatar } from '../pages/Home'
import { accents, AppearanceContext, type Accent } from './AppearanceContext'
import ConversationSidebar from './ConversationSidebar'

type ConversationRoute = { agentID: number; sessionID?: number }

function conversationRoute(pathname: string): ConversationRoute | undefined {
  const match = pathname.match(/^\/agents\/(\d+)\/sessions(?:\/(\d+))?\/?$/)
  if (!match) return undefined
  return { agentID: Number(match[1]), sessionID: match[2] ? Number(match[2]) : undefined }
}

export default function Shell() {
  const navigate = useNavigate()
  const location = useLocation()
  const client = useQueryClient()
  const [menuOpen, setMenuOpen] = useState(false)
  const [conversationsOpen, setConversationsOpen] = useState(false)
  const [accent, setAccent] = useState<Accent>(() => {
    const stored = localStorage.getItem('haro-accent') as Accent | null
    return accents.some(item => item.value === stored) ? stored! : 'teal'
  })
  const agents = useQuery({ queryKey: ['agents'], queryFn: () => api.agents() })
  const currentConversation = conversationRoute(location.pathname)
  const activeAgentMatch = location.pathname.match(/^\/agents\/(\d+)(?:\/|$)/)
  const activeAgentID = activeAgentMatch ? Number(activeAgentMatch[1]) : undefined

  useEffect(() => {
    document.documentElement.dataset.accent = accent
    delete document.documentElement.dataset.theme
    localStorage.setItem('haro-accent', accent)
  }, [accent])
  useEffect(() => { setMenuOpen(false); setConversationsOpen(false) }, [location.pathname])

  const logout = useMutation({ mutationFn: api.logout, onSuccess: () => { client.clear(); navigate('/') } })
  const signOut = () => logout.mutate()

  return <AppearanceContext.Provider value={{ accent, setAccent }}><div className="app-shell">
    <aside className="nav-rail" aria-label="Primary navigation">
      <Link to="/" className="brand-link" aria-label="Haro home"><span className="brand-mark"><Sparkles /></span><span className="brand-name">Haro</span></Link>
      <PrimaryNavigation agents={agents.data?.agents} loading={agents.isLoading} activeAgentID={activeAgentID} pathname={location.pathname} />
      <div className="rail-bottom"><button className="rail-link" onClick={signOut} aria-label="Sign out"><LogOut /><span>Sign out</span></button></div>
    </aside>

    <header className="mobile-topbar">
      <Dialog.Root open={menuOpen} onOpenChange={setMenuOpen}>
        <Dialog.Trigger asChild><button className="topbar-action" aria-label="Open navigation"><Menu /></button></Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Overlay className="nav-drawer-overlay" />
          <Dialog.Content className="nav-drawer" aria-describedby={undefined}>
            <div className="drawer-header"><Dialog.Title><span className="brand-mark"><Sparkles /></span> Haro</Dialog.Title><Dialog.Close className="topbar-action" aria-label="Close navigation"><X /></Dialog.Close></div>
            <PrimaryNavigation agents={agents.data?.agents} loading={agents.isLoading} activeAgentID={activeAgentID} pathname={location.pathname} mobile onNavigate={() => setMenuOpen(false)} />
            <div className="drawer-footer"><button className="drawer-link signout" onClick={signOut}><LogOut /><span>Sign out</span></button></div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      <Link to="/" className="mobile-wordmark">Haro</Link>
      {currentConversation ? <Dialog.Root open={conversationsOpen} onOpenChange={setConversationsOpen}>
        <Dialog.Trigger asChild><button className="topbar-action conversation-trigger" aria-label="Open conversations"><PanelLeft /></button></Dialog.Trigger>
        <Dialog.Portal><Dialog.Overlay className="nav-drawer-overlay conversation-overlay" /><Dialog.Content className="conversation-drawer" aria-describedby={undefined}><Dialog.Title className="sr-only">Conversations</Dialog.Title><Dialog.Close className="conversation-drawer-close" aria-label="Close conversations"><X /></Dialog.Close><ConversationSidebar agentID={currentConversation.agentID} sessionID={currentConversation.sessionID} onNavigate={() => setConversationsOpen(false)} /></Dialog.Content></Dialog.Portal>
      </Dialog.Root> : <span />}
    </header>

    <div className={`app-content ${currentConversation ? 'with-conversations' : ''}`}>
      {currentConversation && <aside className="sessions-panel desktop-conversation-sidebar" aria-label="Conversations"><ConversationSidebar agentID={currentConversation.agentID} sessionID={currentConversation.sessionID} /></aside>}
      <AppRoutes />
    </div>
  </div></AppearanceContext.Provider>
}

function PrimaryNavigation({ agents, loading, activeAgentID, pathname, mobile = false, onNavigate }: { agents?: AgentProfile[]; loading: boolean; activeAgentID?: number; pathname: string; mobile?: boolean; onNavigate?: () => void }) {
  const linkClass = mobile ? 'drawer-link' : 'rail-link'
  const agentsActive = pathname.startsWith('/agents/')
  return <div className={`primary-navigation ${mobile ? 'mobile-primary-navigation' : ''}`}>
    <nav aria-label={mobile ? 'Mobile navigation' : 'Workspace navigation'}>
      <NavLink to="/" end className={({ isActive }) => `${linkClass} ${isActive ? 'active' : ''}`} onClick={onNavigate}><Home /><span>Home</span></NavLink>
    </nav>
    <section className={`primary-agents ${agentsActive ? 'active' : ''}`} aria-labelledby={mobile ? 'mobile-agents-heading' : 'desktop-agents-heading'}>
      <div className="primary-section-heading"><span id={mobile ? 'mobile-agents-heading' : 'desktop-agents-heading'}>Agents</span><Link to="/agents/new" className="small-icon-button" aria-label="New agent" onClick={onNavigate}><Plus /></Link></div>
      <div className="primary-agent-list">{loading ? [1, 2, 3].map(item => <div className="primary-agent-row skeleton" key={item} />) : agents?.length ? agents.map(agent => <Link key={agent.id} to={`/agents/${agent.id}/sessions`} className={`primary-agent-row ${activeAgentID === agent.id ? 'active' : ''}`} onClick={onNavigate}><AgentAvatar agent={agent} /><div><b>{agent.name}</b><span>{agent.model}</span></div></Link>) : <div className="primary-empty">No active agents</div>}</div>
    </section>
    <nav className="primary-resource-nav" aria-label={mobile ? 'Mobile workspace settings' : 'Workspace settings'}>
      <NavLink to="/sandboxes" className={({ isActive }) => `${linkClass} ${isActive ? 'active' : ''}`} onClick={onNavigate}><Box /><span>Sandboxes</span></NavLink>
      <NavLink to="/settings" className={({ isActive }) => `${linkClass} ${isActive ? 'active' : ''}`} onClick={onNavigate}><Settings /><span>Settings</span></NavLink>
    </nav>
  </div>
}
