import { useEffect, useRef } from 'react'
import { useRefine } from '../store'
import type { ScopeQuestion } from '../types'
import { BriefCards, TriageCards } from './Cards'
import { Md } from './Md'

export function Approach({ issueKey, busy }: { issueKey: string; busy: boolean }) {
  const refine = useRefine()
  const t = refine.ticket(issueKey)
  const saved = t.saved!
  // The box and the scope answers start from the last draft's, as the TUI's
  // do.
  const value = t.approach ?? saved.draft?.approach ?? ''
  const scope = t.scope ?? saved.draft?.scope ?? {}
  const questions = saved.triage?.triage.scope_questions ?? []
  const box = useRef<HTMLTextAreaElement>(null)
  useEffect(() => box.current?.focus(), [])

  const setAnswer = (question: string, answer: string) => {
    const next = { ...scope }
    if (answer) next[question] = answer
    else delete next[question]
    refine.patch(issueKey, { scope: next })
  }
  // Only answers to the current questions go to Claude.
  const answers = Object.fromEntries(
    questions.flatMap((q) => (scope[q.question]?.trim() ? [[q.question, scope[q.question].trim()]] : []))
  )
  const submit = () => {
    if (!busy) void refine.ask(issueKey, value, answers)
  }
  const side = saved.triage || saved.brief

  return (
    <div className={side ? 'split' : ''}>
      <div className="stack">
        {questions.length > 0 && (
          <section className="card scope">
            <div className="scope-head">
              <h3>
                Scope · {Object.keys(answers).length}/{questions.length} answered
              </h3>
              <button
                className="link push"
                disabled={busy}
                onClick={() => {
                  refine.track('use_recommendations', issueKey, { unanswered: questions.length - Object.keys(answers).length })
                  const next = { ...scope }
                  for (const q of questions) if (!next[q.question]?.trim()) next[q.question] = q.recommended
                  refine.patch(issueKey, { scope: next })
                }}
              >
                use recommendations
              </button>
            </div>
            <p className="quiet small">These settle what to build, so Claude won't ask them again. Unanswered ones come back as questions.</p>
            {questions.map((q) => (
              <ScopeAnswer
                key={q.question}
                q={q}
                answer={scope[q.question] ?? ''}
                disabled={busy}
                onChange={(answer) => setAnswer(q.question, answer)}
              />
            ))}
          </section>
        )}
        <p className="quiet">
          Write what you know, in a few lines. Mark anything you're unsure of with ? and Claude answers it from the
          code. Empty is fine too.
        </p>
        <textarea
          ref={box}
          className="approach"
          value={value}
          placeholder="How would you build it? Mark anything you're unsure of with ?"
          onChange={(e) => refine.patch(issueKey, { approach: e.target.value })}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && (e.key === 'Enter' || e.key === 's')) {
              e.preventDefault()
              submit()
            }
          }}
          disabled={busy}
        />
        <div className="actions">
          <button className="primary" disabled={busy} onClick={submit}>
            Ask Claude's questions <kbd>⌘↵</kbd>
          </button>
          {saved.draft && <button onClick={() => refine.setTab(issueKey, 'draft')}>Back to the draft</button>}
        </div>
        {saved.brief && (
          <p className="quiet small">Builds on the brief's session{saved.triage ? ' and the triage' : ''}.</p>
        )}
      </div>
      {side && (
        <div className="side">
          {saved.triage && <TriageCards triage={saved.triage.triage} withScope={false} />}
          {saved.brief && <BriefCards brief={saved.brief.brief} />}
        </div>
      )}
    </div>
  )
}

// ScopeAnswer is one scope question: a radio per option plus one for an
// answer in words. An open question gets only the words.
function ScopeAnswer({
  q,
  answer,
  disabled,
  onChange
}: {
  q: ScopeQuestion
  answer: string
  disabled: boolean
  onChange: (answer: string) => void
}) {
  const options = q.options ?? []
  const own = answer !== '' && !options.includes(answer)
  const name = `scope-${q.question}`
  return (
    <fieldset className="scope-q" disabled={disabled}>
      <legend className="scope-ask">
        <Md text={q.question} />
      </legend>
      {options.map((option) => (
        <label key={option} className="scope-option">
          <input type="radio" name={name} checked={answer === option} onChange={() => onChange(option)} />
          <span>
            <Md text={option} />
            {option === q.recommended && (
              <span className="star" title={q.why}>
                {' '}
                ★ recommended{q.why ? ` · ${q.why}` : ''}
              </span>
            )}
          </span>
        </label>
      ))}
      <label className="scope-option own">
        {options.length > 0 && (
          <input type="radio" name={name} checked={own} onChange={() => onChange(own ? answer : ' ')} />
        )}
        <input
          className="own-answer"
          value={own ? answer.trim() === '' ? '' : answer : ''}
          placeholder={options.length > 0 ? 'Other…' : `Your answer · suggested: ${q.recommended}`}
          onFocus={() => {
            if (!own && options.length > 0) onChange(' ')
          }}
          onChange={(e) => onChange(e.target.value || (options.length > 0 ? ' ' : ''))}
        />
      </label>
      {answer && (
        <button className="link" type="button" onClick={() => onChange('')}>
          clear
        </button>
      )}
    </fieldset>
  )
}
