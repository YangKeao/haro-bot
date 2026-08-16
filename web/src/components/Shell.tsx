import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  BookOpenText, Box, Cloud, Home, Library, LogOut, Menu, Palette, Plus, Send, Sparkles, X,
  type LucideIcon,
} from 'lucide-react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import * as Dialog from '@radix-ui/react-dialog'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { api } from '../api'
import { AppRoutes } from '../App'

type Accent = 'blue' | 'teal' | 'green' | 'orange' | 'rose'
const accents: { value: Accent; label: string; color: string }[] = [
  { value: 'teal', label: 'Mist teal', color: '#557d78' },
  { value: 'blue', label: 'Slate blue', color: '#61768e' },
  { value: 'green', label: 'Soft olive', color: '#6f7a58' },
  { value: 'orange', label: 'Warm ochre', color: '#9a7147' },
  { value: 'rose', label: 'Dusty rose', color: '#956a72' },
]

const navigation: { to: string; label: string; Icon: LucideIcon }[] = [
  { to: '/', label: 'Home', Icon: Home },
  { to: '/agents/new', label: 'New agent', Icon: Plus },
  { to: '/guidelines', label: 'Guidelines', Icon: BookOpenText },
  { to: '/skills', label: 'Skills', Icon: Library },
  { to: '/providers', label: 'Providers', Icon: Cloud },
  { to: '/sandboxes', label: 'Sandboxes', Icon: Box },
  { to: '/settings/integrations', label: 'Integrations', Icon: Send },
]

function NavigationLink({ to, label, Icon }: { to: string; label: string; Icon: LucideIcon }) {
  return <NavLink to={to} end={to === '/'} className={({ isActive }) => `rail-link ${isActive ? 'active' : ''}`}><Icon /><span>{label}</span></NavLink>
}

export default function Shell() {
  const navigate = useNavigate()
  const location = useLocation()
  const client = useQueryClient()
  const [menuOpen, setMenuOpen] = useState(false)
  const [accent, setAccent] = useState<Accent>(() => {
    const stored = localStorage.getItem('haro-accent') as Accent | null
    return accents.some(item => item.value === stored) ? stored! : 'teal'
  })
  useEffect(() => {
    document.documentElement.dataset.accent = accent
    delete document.documentElement.dataset.theme
    localStorage.setItem('haro-accent', accent)
  }, [accent])
  useEffect(() => setMenuOpen(false), [location.pathname])
  const logout = useMutation({ mutationFn: api.logout, onSuccess: () => { client.clear(); navigate('/') } })
  const signOut = () => logout.mutate()

  return <div className="app-shell">
    <aside className="nav-rail" aria-label="Primary navigation">
      <Link to="/" className="brand-link" aria-label="Haro home"><span className="brand-mark"><Sparkles /></span><span className="brand-name">Haro</span></Link>
      <nav>{navigation.map(item => <NavigationLink key={item.to} {...item} />)}</nav>
      <div className="rail-bottom">
        <AppearanceMenu accent={accent} onAccent={setAccent} />
        <button className="rail-link" onClick={signOut} aria-label="Sign out"><LogOut /><span>Sign out</span></button>
      </div>
    </aside>

    <header className="mobile-topbar">
      <Dialog.Root open={menuOpen} onOpenChange={setMenuOpen}>
        <Dialog.Trigger asChild><button className="topbar-action" aria-label="Open navigation"><Menu /></button></Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Overlay className="nav-drawer-overlay" />
          <Dialog.Content className="nav-drawer" aria-describedby={undefined}>
            <div className="drawer-header"><Dialog.Title><span className="brand-mark"><Sparkles /></span> Haro</Dialog.Title><Dialog.Close className="topbar-action" aria-label="Close navigation"><X /></Dialog.Close></div>
            <nav aria-label="Mobile navigation">{navigation.map(({ to, label, Icon }) => <Dialog.Close asChild key={to}><NavLink to={to} end={to === '/'} className={({ isActive }) => `drawer-link ${isActive ? 'active' : ''}`}><Icon /><span>{label}</span></NavLink></Dialog.Close>)}</nav>
            <div className="drawer-footer"><div><div className="drawer-label">Accent color</div><AccentChooser accent={accent} onAccent={setAccent} /></div><button className="drawer-link signout" onClick={signOut}><LogOut /><span>Sign out</span></button></div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      <Link to="/" className="mobile-wordmark">Haro</Link>
      <Link to="/agents/new" className="topbar-action" aria-label="New agent"><Plus /></Link>
    </header>

    <div className="app-content"><AppRoutes /></div>
  </div>
}

function AppearanceMenu({ accent, onAccent }: { accent: Accent; onAccent: (accent: Accent) => void }) {
  return <DropdownMenu.Root><DropdownMenu.Trigger asChild><button className="rail-link" aria-label="Appearance"><Palette /><span>Appearance</span></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="dropdown appearance-menu" side="right" align="end" sideOffset={10}><div className="dropdown-heading">Accent color</div>{accents.map(item => <DropdownMenu.Item key={item.value} className={accent === item.value ? 'selected' : ''} onSelect={() => onAccent(item.value)}><span className="accent-swatch" style={{ background: item.color }} />{item.label}{accent === item.value && <span className="selection-mark">✓</span>}</DropdownMenu.Item>)}</DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>
}

function AccentChooser({ accent, onAccent }: { accent: Accent; onAccent: (accent: Accent) => void }) {
  return <div className="accent-chooser">{accents.map(item => <button key={item.value} className={accent === item.value ? 'selected' : ''} aria-label={item.label} title={item.label} style={{ '--swatch': item.color } as React.CSSProperties} onClick={() => onAccent(item.value)} />)}</div>
}
