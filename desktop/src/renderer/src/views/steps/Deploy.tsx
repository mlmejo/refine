import { useState } from 'react'
import { api } from '../../api'
import { useRefine } from '../../store'
import type { Question, View } from '../../types'
import { QuestionCard, useAnswers, useQuestionKeys } from './QuestionCard'

// Deploy asks how each repo without a saved profile deploys, once, then what
// this ticket needs at deploy.
export function Deploy({ view, readOnly }: { view: View; readOnly: boolean }) {
  const refine = useRefine()
  const r = view.refinement
  const key = r.key
  const deploy = r.deploy
  const repoQs = deploy?.repos ?? []
  const ticketQs = deploy?.ticket ?? []
  const all: Question[] = [...repoQs, ...ticketQs]
  const { answers, set, flush } = useAnswers(
    all.map((q) => q.answer ?? ''),
    (answers) =>
      refine.act(key, 'answers', () =>
        api.answer(key, { repos: answers.slice(0, repoQs.length), ticket: answers.slice(repoQs.length) })
      )
  )
  const [focus, setFocus] = useState(Math.max(answers.findIndex((a) => !a), 0))
  const left = answers.filter((a) => !a.trim()).length

  const generate = async () => {
    if (left > 0 || refine.pending) return
    await flush()
    void refine.run('generate', key)
  }
  useQuestionKeys(all, answers, focus, setFocus, set, () => void generate(), !readOnly && !!deploy)

  if (!deploy) {
    return (
      <div className="scroll">
        <div className="working">
          <p className="muted">The deployment questions have not been asked yet.</p>
          <div>
            <button className="btn primary" onClick={() => void refine.run('deploy', key)}>
              Ask deployment questions
            </button>
          </div>
        </div>
      </div>
    )
  }

  const repos = [...new Set(repoQs.map((q) => q.repo))]
  const card = (q: Question, i: number, n: number) => (
    <QuestionCard
      key={i}
      n={n}
      question={q}
      answer={answers[i] ?? ''}
      focused={i === focus}
      readOnly={readOnly}
      onFocus={() => setFocus(i)}
      onAnswer={(a) => set(i, a)}
    />
  )

  return (
    <>
      <div className="scroll">
        <div className="page">
          <div className="page-main">
            {repos.map((repo) => (
              <section key={repo} className="section">
                <div className="section-head">
                  <h2>
                    How <span className="mono">{repo}</span> deploys
                  </h2>
                  <span className="muted small">Asked once. Saved for this repo and reused on later tickets.</span>
                </div>
                {repoQs.map((q, i) => (q.repo === repo ? card(q, i, repoQs.filter((x, j) => x.repo === repo && j <= i).length) : null))}
              </section>
            ))}
            <section className="section">
              <div className="section-head">
                <h2>What {key} needs at deploy</h2>
                <span className="muted small">
                  {ticketQs.length ? 'One round, at most 5 questions' : 'Claude found nothing this ticket adds to a deploy.'}
                </span>
              </div>
              {ticketQs.map((q, i) => card(q, repoQs.length + i, i + 1))}
            </section>
          </div>
          <aside className="page-side">
            {(view.profiles ?? []).length > 0 && (
              <div className="card">
                <h3>Already saved</h3>
                {(view.profiles ?? []).map((p) => (
                  <div key={p.repo} className="stack" style={{ gap: 2 }}>
                    <span className="mono">{p.repo}</span>
                    {(p.answers ?? []).map((qa) => (
                      <span key={qa.question} className="muted small">
                        {qa.question} <span style={{ color: 'var(--text-2)' }}>{qa.answer}</span>
                      </span>
                    ))}
                  </div>
                ))}
                <button className="link" style={{ alignSelf: 'flex-start' }} onClick={() => refine.setScreen({ kind: 'repos' })}>
                  Edit in Repos
                </button>
              </div>
            )}
            <p className="muted small" style={{ margin: 0 }}>
              Deploy needs go into one comment on {key} when you stamp.
            </p>
          </aside>
        </div>
      </div>
      {!readOnly && (
        <footer className="footbar">
          <span className="muted small">
            <kbd>1</kbd>–<kbd>4</kbd> pick
          </span>
          <span className="muted small">
            <kbd>R</kbd> pick recommended
          </span>
          <span className="small" style={{ marginLeft: 'auto', color: left ? 'var(--amber)' : 'var(--muted)' }}>
            {left ? `${left} question${left === 1 ? '' : 's'} left` : 'All answered'}
          </span>
          <button className="btn primary" disabled={left > 0 || !!refine.pending} onClick={() => void generate()}>
            Generate AC and TI <kbd>⌘↵</kbd>
          </button>
        </footer>
      )}
    </>
  )
}
