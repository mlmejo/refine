import { api } from '../../api'
import { useRefine } from '../../store'
import type { View } from '../../types'

// StampDialog lists every Jira write a stamp makes and runs them. A failed
// write shows its error, and a retry starts again from it.
export function StampDialog({ view, onClose }: { view: View; onClose: () => void }) {
  const refine = useRefine()
  const r = view.refinement
  const plan = view.stamp_plan ?? []
  const done = r.stamp.done ?? []
  const working = refine.pending === 'stamp'
  const started = done.length > 0 || !!r.stamp.failed

  const stamp = async () => {
    const after = await refine.act(r.key, 'stamp', () => api.stamp(r.key))
    if (after?.refinement.step === 'stamped') {
      void refine.refresh()
      onClose()
    }
  }

  return (
    <div className="overlay" onClick={() => !working && onClose()}>
      <section role="dialog" aria-labelledby="stamp-title" className="dialog" onClick={(e) => e.stopPropagation()}>
        <div className="dialog-head">
          <h2 id="stamp-title">Stamp {r.key}</h2>
          <p className="muted small" style={{ margin: 0 }}>
            Writes to Jira in this order. A retry skips the steps already done.
          </p>
        </div>
        <ol className="plan">
          {plan.map((a, i) => {
            const state = done.includes(a.id) ? 'done' : r.stamp.failed === a.id ? 'failed' : 'todo'
            const next = working && state === 'todo' && plan.findIndex((p) => !done.includes(p.id)) === i
            return (
              <li key={a.id} className={state}>
                {next ? <span className="spinner" /> : <span className={`dot ${state}`}>{state === 'done' ? '✓' : state === 'failed' ? '!' : ''}</span>}
                <span className="stack" style={{ gap: 2 }}>
                  <span>{a.label}</span>
                  {state === 'failed' && <span className="small error-text">{r.stamp.error}</span>}
                </span>
              </li>
            )
          })}
        </ol>
        <div className="dialog-foot">
          <span className="muted small">
            {done.length} of {plan.length} done
          </span>
          <button className="btn" style={{ marginLeft: 'auto' }} disabled={working} onClick={onClose}>
            Close
          </button>
          <button className="btn primary" disabled={working} onClick={() => void stamp()}>
            {working ? 'Stamping…' : r.stamp.failed ? `Retry from step ${plan.findIndex((p) => p.id === r.stamp.failed) + 1}` : started ? 'Continue stamping' : 'Stamp'}
          </button>
        </div>
      </section>
    </div>
  )
}
