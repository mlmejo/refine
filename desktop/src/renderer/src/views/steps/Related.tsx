import { useEffect, useState } from 'react'
import { api } from '../../api'
import { useRefine } from '../../store'
import type { Blocker, View } from '../../types'
import { typing } from '../../typing'

// Related reviews the blockers and overlaps Claude found. Accepted blockers
// that are not refined make the ticket wait.
export function Related({ view, readOnly }: { view: View; readOnly: boolean }) {
  const refine = useRefine()
  const r = view.refinement
  const key = r.key
  const blockers = r.related.blockers ?? []
  const overlaps = r.related.overlaps ?? []
  const waiting = r.related.waiting ?? []
  const undecided = blockers.filter((b) => !b.decision).length
  const [explanation, setExplanation] = useState('')
  const [focus, setFocus] = useState(0)

  const decide = (b: Blocker, decision: 'accepted' | 'rejected') =>
    void refine.act(key, 'decide', () => api.decide(key, b.key, decision))
  const proceed = () => void refine.act(key, 'continue', () => api.proceed(key))

  useEffect(() => {
    if (readOnly) return
    const onKey = (e: KeyboardEvent) => {
      if (refine.palette || typing(e)) return
      const b = blockers[focus]
      if (e.key === 'a' && b) decide(b, 'accepted')
      else if (e.key === 'x' && b) decide(b, 'rejected')
      else if (e.key === 'j') setFocus((f) => Math.min(f + 1, blockers.length - 1))
      else if (e.key === 'k') setFocus((f) => Math.max(f - 1, 0))
      else if (e.key === 'Enter' && e.metaKey && undecided === 0) proceed()
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  return (
    <>
      <div className="scroll">
        <div className="page">
          <div className="page-main">
            <section className="section">
              <div className="section-head">
                <h2>Blockers</h2>
                <span className="muted small">
                  {blockers.length === 0
                    ? 'Claude found none in your open tickets.'
                    : 'Accept the tickets this one builds on. Their work is treated as done, and never asked about.'}
                </span>
              </div>
              {blockers.map((b, i) => (
                <div key={b.key} className="card red" style={i === focus && !readOnly ? { borderColor: 'var(--red)' } : undefined} onClick={() => setFocus(i)}>
                  <div className="row" style={{ flexWrap: 'wrap' }}>
                    <span className="mono muted">{b.key}</span>
                    <span style={{ fontWeight: 500 }}>{b.summary}</span>
                    <span className={`chip ${b.refined ? 'green' : 'amber'}`}>{b.refined ? 'Refined' : 'Not refined'}</span>
                    {b.from_code && <span className="chip">found in the code</span>}
                    {!readOnly && (
                      <div className="row" style={{ marginLeft: 'auto', gap: 4 }}>
                        <button className="btn toggle" aria-pressed={b.decision === 'accepted'} onClick={() => decide(b, 'accepted')}>
                          {b.decision === 'accepted' ? 'Accepted' : 'Accept'} <kbd>A</kbd>
                        </button>
                        <button className="btn toggle" aria-pressed={b.decision === 'rejected'} onClick={() => decide(b, 'rejected')}>
                          {b.decision === 'rejected' ? 'Rejected' : 'Reject'} <kbd>X</kbd>
                        </button>
                      </div>
                    )}
                    {readOnly && <span className="chip">{b.decision}</span>}
                  </div>
                  <p style={{ margin: 0, color: 'var(--text-2)' }}>{b.reason}</p>
                  {b.quote && <p className="quote">“{b.quote}”</p>}
                  {!b.refined && b.decision === 'accepted' && (
                    <div className="banner">
                      <span className="grow">This ticket waits until {b.key} is refined.</span>
                      <button className="link" style={{ color: 'inherit' }} onClick={() => refine.open(b.key)}>
                        Refine {b.key} first →
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </section>

            {overlaps.length > 0 && (
              <section className="section">
                <div className="section-head">
                  <h2>Overlaps</h2>
                  <span className="muted small">For your information. Resolve these in Jira.</span>
                </div>
                {overlaps.map((o) => (
                  <div key={o.key} className="card">
                    <div className="row" style={{ flexWrap: 'wrap' }}>
                      <span className="mono muted">{o.key}</span>
                      <span>{o.summary}</span>
                      {refine.info?.site && (
                        <a className="small" style={{ marginLeft: 'auto' }} href={`https://${refine.info.site}/browse/${o.key}`} target="_blank" rel="noreferrer">
                          Open in Jira ↗
                        </a>
                      )}
                    </div>
                    <span className="muted small">{o.reason}</span>
                  </div>
                ))}
              </section>
            )}

            {!readOnly && (
              <section className="section">
                <label htmlFor="explain" style={{ fontWeight: 600 }}>
                  Something wrong?
                </label>
                <textarea
                  id="explain"
                  className="textarea"
                  rows={3}
                  value={explanation}
                  placeholder="Tell Claude what it missed or got wrong, e.g. “GEEK-131 also blocks this, it adds the refund reason column.”"
                  onChange={(e) => setExplanation(e.target.value)}
                />
                <div>
                  <button className="btn" disabled={!explanation.trim()} onClick={() => void refine.run('search', key, explanation)}>
                    Search again
                  </button>
                </div>
                {(r.related.explanations ?? []).length > 0 && (
                  <div className="notice">
                    You told Claude: {(r.related.explanations ?? []).map((e) => `“${e}”`).join(' · ')}
                  </div>
                )}
              </section>
            )}
          </div>
        </div>
      </div>
      {!readOnly && (
        <footer className="footbar">
          {waiting.length > 0 ? (
            <span className="small" style={{ color: 'var(--amber)' }}>
              Waiting on {waiting.join(', ')}. Refine {waiting.length === 1 ? 'it' : 'them'} first, then check again.
            </span>
          ) : (
            <span className="muted small">
              {undecided > 0 ? `${blockers.length - undecided} of ${blockers.length} blockers decided` : 'Every blocker decided'}
            </span>
          )}
          <button className="btn primary" style={{ marginLeft: 'auto' }} disabled={undecided > 0 || !!refine.pending} onClick={proceed}>
            {waiting.length > 0 ? 'Check again' : 'Continue to repos'} <kbd>⌘↵</kbd>
          </button>
        </footer>
      )}
    </>
  )
}
