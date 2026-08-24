import { createContext, useContext } from 'react'

export type Accent = 'blue' | 'teal' | 'green' | 'orange' | 'rose'

export const accents: { value: Accent; label: string; color: string }[] = [
  { value: 'teal', label: 'Mist teal', color: '#557d78' },
  { value: 'blue', label: 'Slate blue', color: '#61768e' },
  { value: 'green', label: 'Soft olive', color: '#6f7a58' },
  { value: 'orange', label: 'Warm ochre', color: '#9a7147' },
  { value: 'rose', label: 'Dusty rose', color: '#956a72' },
]

type AppearanceValue = {
  accent: Accent
  setAccent: (accent: Accent) => void
}

export const AppearanceContext = createContext<AppearanceValue | null>(null)

export function useAppearance() {
  const value = useContext(AppearanceContext)
  if (!value) throw new Error('useAppearance must be used inside AppearanceContext.Provider')
  return value
}
