import { useEffect, useState } from 'react'
import type { BackendState } from '../../preload/index.d'
import { RefineProvider, useRefine } from './store'
import { Sidebar } from './views/Sidebar'
import { TicketPane } from './views/TicketPane'
import { SyncDialog } from './views/SyncDialog'
import { ReposDialog } from './views/ReposDialog'

export function App() {
  const [state, setState] = useState<BackendState>({ status: 'starting' })
  useEffect(() => {
    void window.refine.backendState().then(setState)
    return window.refine.onBackendState(setState)
  }, [])

  if (state.status === 'starting') {
    return (
      <div className="boot">
        <span className="spinner" /> Starting refine…
      </div>
    )
  }
  if (state.status === 'failed') {
    return (
      <div className="boot">
        <div className="boot-card">
          <h2>refine could not start</h2>
          <pre className="error-text">{state.error}</pre>
          <p className="quiet">
            Check ~/.config/refine/config.toml and that the refine binary is built (pnpm go:build).
          </p>
          <button className="primary" onClick={() => void window.refine.restartBackend().then(setState)}>
            Try again
          </button>
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
  const [repos, setRepos] = useState(false)
  return (
    <div className="shell">
      <Sidebar onRepos={() => setRepos(true)} />
      <main className="main">
        {refine.selected ? (
          <TicketPane key={refine.selected} issueKey={refine.selected} />
        ) : (
          <div className="empty">
            <p>Pick a ticket, or open one by key.</p>
            {refine.flash && <p className={refine.flash.error ? 'error-text' : 'done-text'}>{refine.flash.text}</p>}
          </div>
        )}
      </main>
      {refine.syncAsk && <SyncDialog />}
      {repos && <ReposDialog onClose={() => setRepos(false)} />}
    </div>
  )
}
