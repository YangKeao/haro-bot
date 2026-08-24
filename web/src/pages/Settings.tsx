import { useEffect } from 'react'
import { BookOpenText, Cloud, Library, Palette, Send } from 'lucide-react'
import { useLocation } from 'react-router-dom'
import { accents, useAppearance } from '../components/AppearanceContext'
import Guidelines from './Guidelines'
import Integrations from './Integrations'
import Providers from './Providers'
import Skills from './Skills'

const sections = [
  { id: 'appearance', label: 'Appearance', Icon: Palette },
  { id: 'providers', label: 'Providers', Icon: Cloud },
  { id: 'guidelines', label: 'Guidelines', Icon: BookOpenText },
  { id: 'skills', label: 'Skills', Icon: Library },
  { id: 'integrations', label: 'Integrations', Icon: Send },
]

export default function Settings() {
  const location = useLocation()
  const { accent, setAccent } = useAppearance()

  useEffect(() => {
    if (!location.hash) return
    const frame = requestAnimationFrame(() => document.getElementById(location.hash.slice(1))?.scrollIntoView())
    return () => cancelAnimationFrame(frame)
  }, [location.hash])

  return <main className="page scroll-page settings-page settings-hub-page">
    <header className="page-header settings-hub-header"><div><div className="eyebrow">Workspace configuration</div><h1>Settings</h1><p className="muted">Manage shared connections, behavior, capabilities and integrations.</p></div></header>
    <div className="settings-hub-layout">
      <nav className="settings-outline settings-hub-outline" aria-label="Settings sections">{sections.map(section => <a href={`#${section.id}`} key={section.id}><section.Icon /><span>{section.label}</span></a>)}</nav>
      <div className="settings-hub-content">
        <section id="appearance" className="settings-hub-section">
          <header className="page-header"><div><div className="eyebrow">Personalization</div><h2>Appearance</h2><p className="muted">Choose the accent used throughout this browser.</p></div></header>
          <div className="settings-card appearance-card"><div className="section-heading"><h3>Accent color</h3><p>The preference is applied immediately and stored locally.</p></div><div className="appearance-options" role="radiogroup" aria-label="Accent color">{accents.map(item => <button key={item.value} role="radio" aria-checked={accent === item.value} className={accent === item.value ? 'selected' : ''} onClick={() => setAccent(item.value)}><span className="accent-swatch large" style={{ background: item.color }} /><span>{item.label}</span>{accent === item.value && <b>Selected</b>}</button>)}</div></div>
        </section>
        <section id="providers" className="settings-hub-section"><Providers embedded /></section>
        <section id="guidelines" className="settings-hub-section"><Guidelines embedded /></section>
        <section id="skills" className="settings-hub-section"><Skills embedded /></section>
        <section id="integrations" className="settings-hub-section"><Integrations embedded /></section>
      </div>
    </div>
  </main>
}
