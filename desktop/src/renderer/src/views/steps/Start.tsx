import { useRefine } from '../../store'
import type { View } from '../../types'
import { Summary } from './Summary'

// Start shows a ticket not refined yet, and starts its refinement.
export function Start({ view }: { view: View }) {
  const refine = useRefine()
  const t = view.ticket
  return (
    <>
      <div className="scroll">
        <Summary view={view} />
      </div>
      <footer className="footbar">
        <span className="muted small">First, Claude looks for tickets this one builds on or overlaps, from Jira alone.</span>
        <button className="btn primary" style={{ marginLeft: 'auto' }} onClick={() => void refine.run('search', t.key)}>
          Start refinement
        </button>
      </footer>
    </>
  )
}
