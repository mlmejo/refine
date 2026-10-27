import { useEffect, useState } from 'react'
import type { BackendState } from '../../preload/index.d'
import { RefineProvider, useRefine } from './store'
import { Nav } from './views/Nav'
import { Tickets } from './views/Tickets'
import { Ticket } from './views/Ticket'
import { ReposScreen } from './views/ReposScreen'
import { ClaudeScreen } from './views/ClaudeScreen'
import { SetupScreen } from './views/SetupScreen'
import { api } from './api'
import type { ClaudeDir } from './types'
import { Palette } from './views/Palette'

export function App() {
  const [state, setState] = useState<BackendState>({ status: 'starting' })
  useEffect(() => {
    void window.refine.backendState().then(setState)
    return window.refine.onBackendState(setState)
  }, [])

  if (state.status === 'starting') {
    return (
      <div className="boot">
        <div className="row">
          <span className="spinner" /> Starting refine…
        </div>
      </div>
    )
  }
  if (state.status === 'setup') return <SetupScreen />
  if (state.status === 'failed') {
    return (
      <div className="boot">
        <div className="boot-card">
          <h2>refine could not start</h2>
          <pre className="error-text">{state.error}</pre>
          <p className="muted">
            Check ~/.config/refine/config.toml and that the refine binary is built (pnpm go:build).
          </p>
          <div>
            <button className="btn primary" onClick={() => void window.refine.restartBackend().then(setState)}>
              Try again
            </button>
          </div>
        </div>
      </div>
    )
  }
  return (
    <RefineProvider>
      <Shell />
    </RefineProvider>
  )
}

function Shell() {
  const refine = useRefine()
  const { setPalette, screen, setScreen } = refine
  const [claude, setClaude] = useState<ClaudeDir>()

  useEffect(() => {
    // Without the Claude config folder no run can start, so a missing one is
    // asked for before anything else.
    void api.claudeDir().then(setClaude, () => undefined)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setPalette])

  useEffect(() => {
    if ('Notification' in window && Notification.permission === 'default') void Notification.requestPermission()
  }, [])

  if (claude && !claude.found) return <ClaudeScreen claude={claude} onSaved={setClaude} />

  return (
    <div className="shell">
      <Nav />
      <main className="main">
        {screen.kind === 'tickets' && <Tickets />}
        {screen.kind === 'ticket' && <Ticket key={screen.key} ticketKey={screen.key} />}
        {screen.kind === 'repos' && <ReposScreen onBack={() => setScreen({ kind: 'tickets' })} />}
        {screen.kind === 'claude' && claude && <ClaudeScreen claude={claude} onSaved={setClaude} onBack={() => setScreen({ kind: 'tickets' })} />}
      </main>
      {refine.palette && <Palette />}
      {refine.toast && <div className={`toast ${refine.toast.error ? 'error-text' : ''}`}>{refine.toast.text}</div>}
    </div>
  )
}
