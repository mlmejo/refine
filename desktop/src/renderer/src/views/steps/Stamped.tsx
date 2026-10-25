import { useRefine } from '../../store'
import type { View } from '../../types'

// Stamped is a refinement written to Jira.
export function Stamped({ view }: { view: View }) {
  const refine = useRefine()
  const r = view.refinement
  const children = Object.values(r.stamp.children ?? {})
  return (
    <div className="scroll">
      <div className="working">
        <h2 style={{ margin: 0, fontSize: 14 }}>
          {r.key} is stamped
        </h2>
        <p className="muted" style={{ margin: 0 }}>
          Written to Jira and moved to {refine.info?.ready_sprint}.
          {children.length > 0 && ` Split tickets: ${children.join(', ')}.`}
        </p>
        <div className="row">
          <button className="btn primary" onClick={() => refine.setScreen({ kind: 'tickets' })}>
            Back to tickets
          </button>
        </div>
      </div>
    </div>
  )
}
