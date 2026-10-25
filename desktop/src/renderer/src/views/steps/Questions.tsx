import { useState } from 'react'
import { api } from '../../api'
import { useRefine } from '../../store'
import type { View } from '../../types'
import { QuestionCard, useAnswers, useQuestionKeys } from './QuestionCard'

// Questions is the current round. Every question needs an answer before the
// next round; a round with no questions moves on to deployment.
export function Questions({ view, readOnly }: { view: View; readOnly: boolean }) {
  const refine = useRefine()
  const r = view.refinement
  const key = r.key
  const rounds = r.rounds ?? []
  const max = refine.info?.max_rounds ?? 3
  const [shown, setShown] = useState(Math.max(rounds.length - 1, 0))
  const round = rounds[shown]
  const current = shown === rounds.length - 1 && !readOnly
  const questions = round?.questions ?? []
  const { answers, set, flush } = useAnswers(
    questions.map((q) => q.answer ?? ''),
    (answers) => refine.act(key, 'answers', () => api.answer(key, { round: answers }))
  )
  const [focus, setFocus] = useState(Math.max(answers.findIndex((a) => !a), 0))
  const left = answers.filter((a) => !a.trim()).length
  const pending = (r.related.blockers ?? []).filter((b) => b.from_code && !b.decision)

  const submit = async () => {
    if (left > 0 || pending.length > 0 || refine.pending) return
    await flush()
    void refine.run('ask', key)
  }
  useQuestionKeys(questions, answers, focus, setFocus, set, () => void submit(), current)

  const found = pending.length > 0 && !readOnly && (
    <section className="section">
      <div className="section-head">
        <h2>Found while reading the code</h2>
        <span className="muted small">Accepting one starts the questions over, with its work treated as done.</span>
      </div>
      {pending.map((b) => (
        <div key={b.key} className="card red">
          <div className="row" style={{ flexWrap: 'wrap' }}>
            <span className="mono muted">{b.key}</span>
            <span style={{ fontWeight: 500 }}>{b.summary}</span>
            <span className={`chip ${b.refined ? 'green' : 'amber'}`}>{b.refined ? 'Refined' : 'Not refined'}</span>
            <div className="row" style={{ marginLeft: 'auto', gap: 4 }}>
              <button className="btn" onClick={() => void refine.act(key, 'decide', () => api.decide(key, b.key, 'accepted'))}>
                Accept
              </button>
              <button className="btn" onClick={() => void refine.act(key, 'decide', () => api.decide(key, b.key, 'rejected'))}>
                Reject
              </button>
            </div>
          </div>
          <p style={{ margin: 0, color: 'var(--text-2)' }}>{b.reason}</p>
        </div>
      ))}
    </section>
  )

  if (rounds.length === 0) {
    if (found) {
      return (
        <div className="scroll">
          <div className="page">
            <div className="page-main">
              {found}
              <div>
                <button className="btn primary" disabled={pending.length > 0} onClick={() => void refine.run('ask', key)}>
                  Ask the questions
                </button>
              </div>
            </div>
          </div>
        </div>
      )
    }
    return (
      <div className="scroll">
        <div className="working">
          <p className="muted">No questions yet.</p>
          <div>
            <button className="btn primary" onClick={() => void refine.run('ask', key)}>
              Start round 1
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <>
      <div className="scroll">
        <div className="page">
          <section className="page-main" style={{ gap: 12 }}>
            {found}
            <div className="row" style={{ gap: 12, flexWrap: 'wrap' }}>
              <h2 style={{ margin: 0, fontSize: 13 }}>
                Round {shown + 1} of {max}
              </h2>
              <div className="progress" aria-hidden="true">
                {questions.map((_, i) => (
                  <span key={i} className={answers[i]?.trim() ? 'on' : ''} />
                ))}
              </div>
              <span className="muted small">
                {questions.length - left} of {questions.length} answered
              </span>
            </div>
            {questions.map((q, i) => (
              <QuestionCard
                key={`${shown}-${i}`}
                n={i + 1}
                question={q}
                answer={current ? (answers[i] ?? '') : (q.answer ?? '')}
                focused={i === focus}
                readOnly={!current}
                onFocus={() => setFocus(i)}
                onAnswer={(a) => set(i, a)}
              />
            ))}
          </section>
          <aside className="page-side">
            {(r.related.blockers ?? []).some((b) => b.decision === 'accepted') && (
              <div className="card">
                <h3>Not asked, delivered elsewhere</h3>
                {(r.related.blockers ?? [])
                  .filter((b) => b.decision === 'accepted')
                  .map((b) => (
                    <span key={b.key} className="small">
                      <span className="mono muted">{b.key}</span> {b.summary}
                    </span>
                  ))}
              </div>
            )}
            {rounds.length > 1 && (
              <div className="card">
                <h3>Rounds</h3>
                {rounds.map((_, i) => (
                  <button key={i} className="link" style={{ textAlign: 'left' }} onClick={() => setShown(i)}>
                    {i === shown ? '› ' : ''}Round {i + 1}
                    {i === rounds.length - 1 ? ' (current)' : ''}
                  </button>
                ))}
              </div>
            )}
          </aside>
        </div>
      </div>
      {current && (
        <footer className="footbar">
          <span className="muted small">
            <kbd>1</kbd>–<kbd>4</kbd> pick
          </span>
          <span className="muted small">
            <kbd>R</kbd> pick recommended
          </span>
          <span className="small" style={{ marginLeft: 'auto', color: left || pending.length ? 'var(--amber)' : 'var(--muted)' }}>
            {pending.length
              ? `Decide ${pending.map((b) => b.key).join(', ')} first`
              : left
                ? `${left} question${left === 1 ? '' : 's'} left`
                : rounds.length >= max
                  ? 'Last round answered'
                  : 'All answered'}
          </span>
          <button className="btn primary" disabled={left > 0 || pending.length > 0 || !!refine.pending} onClick={() => void submit()}>
            Submit round <kbd>⌘↵</kbd>
          </button>
        </footer>
      )}
    </>
  )
}
