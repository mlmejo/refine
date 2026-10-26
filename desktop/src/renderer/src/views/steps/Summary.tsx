import type { View } from '../../types'
import { MdDoc } from '../MdDoc'

// Summary shows the ticket as Jira has it: its description and links.
export function Summary({ view }: { view: View }) {
  const t = view.ticket
  return (
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
  )
}
