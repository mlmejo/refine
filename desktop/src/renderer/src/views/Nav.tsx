import { useRefine } from '../store'
import { Icon } from './Icon'

// Nav is the sidebar: search, then the workspace's places.
export function Nav() {
  const refine = useRefine()
  const { screen } = refine
  const onTickets = screen.kind === 'tickets' || screen.kind === 'ticket'
  return (
    <nav className="nav" aria-label="Main">
      <div className="brand">
        <span className="brand-mark">R</span>
        Refine
      </div>
      <button className="search-btn" onClick={() => refine.setPalette(true)}>
        <Icon name="search" />
        <span className="grow">Search or jump to…</span>
        <kbd>⌘K</kbd>
      </button>
      <div className="nav-section">
        <span className="nav-label">Workspace</span>
        <button className={`nav-item ${onTickets ? 'active' : ''}`} onClick={() => refine.setScreen({ kind: 'tickets' })}>
          <Icon name="tickets" />
          Tickets
          <span className="count">{refine.page?.rows?.length ?? ''}</span>
        </button>
        <button className={`nav-item ${screen.kind === 'repos' ? 'active' : ''}`} onClick={() => refine.setScreen({ kind: 'repos' })}>
          <Icon name="folder" />
          Repos
        </button>
        <button className={`nav-item ${screen.kind === 'claude' ? 'active' : ''}`} onClick={() => refine.setScreen({ kind: 'claude' })}>
          <Icon name="account" />
          Claude
        </button>
      </div>
      <div className="nav-foot">
        <span>{refine.info?.sprint}</span>
        <span>Stamps into {refine.info?.ready_sprint}</span>
      </div>
    </nav>
  )
}
