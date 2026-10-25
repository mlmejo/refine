import { useRefine } from '../../store'
import type { View } from '../../types'
import { MdDoc } from '../MdDoc'

// Start shows a ticket not refined yet, and starts its refinement.
export function Start({ view }: { view: View }) {
  const refine = useRefine()
  const t = view.ticket
  return (
    <>
      <div className="scroll">
        <div className="page">
          <div className="page-main">
            <section className="section">
              <div className="section-head">
                <h2>Description</h2>
                <span className="muted small">{t.status}</span>
              </div>
              {t.description_md ? <MdDoc text={t.description_md} /> : <p className="muted">No description.</p>}
            </section>
          </div>
          {(t.links ?? []).length > 0 && (
            <aside className="page-side">
              <div className="card">
                <h3>Links in Jira</h3>
                {(t.links ?? []).map((l) => (
                  <span key={l.key + l.relation} className="small">
                    <span className="muted">{l.relation}</span> <span className="mono">{l.key}</span> {l.summary}
                  </span>
                ))}
              </div>
            </aside>
          )}
        </div>
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
