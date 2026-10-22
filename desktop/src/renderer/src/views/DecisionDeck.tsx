import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, CircleHelp, CornerDownRight, OctagonAlert, RefreshCw, RotateCcw, TextQuote } from 'lucide-react'
import { api } from '../api'
import { useRefine } from '../store'
import { UNKNOWN, unknownNote, type Anchor, type Choice, type Decided, type Saved } from '../types'
import { BlockerText } from './Blocker'
import { Md } from './Md'

// Decision is one thing the developer decides: a deploy blocker, or an open
// question with options.
export type Decision = Choice & { kind: 'blocker' | 'question'; index: number }

// A blocker states a fact someone must settle, not a question, so the
// developer answers it by saying who handles it.
const blockerOptions = ['Part of this ticket: I arrange it', 'Already in place', 'Someone else settles it first']
const blockerConsequences = [
  'Becomes a requirement of this ticket: you arrange it with whoever owns it.',
  'Nothing to arrange; the write-up treats it as done.',
  'Left out of this ticket; it must be settled before the sprint starts.'
]

export function decisions(saved: Saved): Decision[] {
  const d = saved.draft
  return [
    ...(d.deployment.blockers ?? []).map((q, index) => ({
      question: q,
      options: blockerOptions,
      consequences: blockerConsequences,
      recommended: '',
      why: '',
      kind: 'blocker' as const,
      index
    })),
    ...(d.open_questions ?? []).map((q, index) => ({ ...q, kind: 'question' as const, index }))
  ]
}

const answerOf = (saved: Saved, q: Decision) => (saved.answers?.[q.question] ?? '').trim()

// agrees reports whether an answer took Claude's recommendation.
export function agrees(q: Decision, answer: string): boolean {
  return q.recommended !== '' && answer.toLowerCase() === q.recommended.trim().toLowerCase()
}

// DecisionDeck asks one decision at a time. Each option says what it leads
// to; Claude's recommendation is never shown, so the developer decides on
// the consequences, not on whose pick it is. The findings a question rests
// on sit beside it, folded.
export function DecisionDeck({
  issueKey,
  saved,
  busy,
  onShowTicket
}: {
  issueKey: string
  saved: Saved
  busy: boolean
  // onShowTicket opens the ticket beside the deck at the anchor's quote.
  onShowTicket: (anchor: Anchor | null) => void
}) {
  const refine = useRefine()
  const items = decisions(saved)
  const findings = saved.draft.findings ?? []
  const firstOpen = items.findIndex((q) => !answerOf(saved, q))
  const [at, setAt] = useState(firstOpen < 0 ? 0 : firstOpen)
  const [words, setWords] = useState<string>()
  const [findOut, setFindOut] = useState<string>()
  const [error, setError] = useState<string>()
  const shownAt = useRef(Date.now())
  const opened = useRef(false)
  const q = items[Math.min(at, items.length - 1)]
  const answer = q ? answerOf(saved, q) : ''
  const options = q?.options ?? []
  const unknown = unknownNote(answer)
  const own = answer !== '' && !options.includes(answer) && unknown === null

  useEffect(() => {
    shownAt.current = Date.now()
    opened.current = false
    setWords(undefined)
    setFindOut(undefined)
    setError(undefined)
  }, [at, saved.id])

  // Keys: 1-9 pick an option, arrows move, unless typing.
  useKeys((e) => {
    if (!q || busy || words !== undefined || findOut !== undefined) return
    const n = Number(e.key)
    if (n >= 1 && n <= options.length) void decide(options[n - 1], 'option')
    else if (e.key === 'ArrowRight') go(at + 1)
    else if (e.key === 'ArrowLeft') go(at - 1)
  })

  if (!q) return null

  const decide = async (value: string, how: 'option' | 'words' | 'unknown') => {
    if (value === answer) return
    const first = answer === ''
    try {
      const next = await api.answers(issueKey, saved.id, { ...(saved.answers ?? {}), [q.question]: value })
      refine.setDraft(issueKey, next)
      setWords(undefined)
      setFindOut(undefined)
      setError(undefined)
      refine.track('decide', issueKey, {
        kind: q.kind,
        index: q.index,
        how,
        first,
        ms: Date.now() - shownAt.current,
        evidence_opened: opened.current,
        evidence: q.evidence?.length ?? 0,
        agreed: value === '' || how === 'unknown' ? null : agrees(q, value),
        anchor: q.anchor?.source ?? '',
        arm: saved.draft.arm ?? '',
        reopens: q.reopens != null && q.reopens >= 0
      })
    } catch (err) {
      setError(`could not save the answer: ${(err as Error).message}`)
    }
  }

  const go = (i: number) => {
    const next = Math.max(0, Math.min(items.length - 1, i))
    if (next !== at) refine.track('deck_nav', issueKey, { from: at, to: next })
    setAt(next)
  }
  const nextOpen = () => {
    const after = items.findIndex((x, i) => i > at && !answerOf(saved, x))
    const any = items.findIndex((x) => !answerOf(saved, x))
    go(after >= 0 ? after : any >= 0 ? any : at + 1)
  }

  // handled are decisions made or marked "I don't know yet"; both let the
  // round go on, only the first settles anything.
  const decided = items.filter((x) => answerOf(saved, x)).length
  const toFindOut = items.filter((x) => unknownNote(answerOf(saved, x)) !== null).length
  const evidence = (q.evidence ?? []).filter((i) => i >= 0 && i < findings.length)
  const log = saved.draft.decided ?? []
  const reopened = q.reopens != null && q.reopens >= 0 ? log[q.reopens] : undefined

  return (
    <section className="deck">
      <div className="deck-head">
        <span className="deck-count">
          Decision {at + 1} of {items.length}
        </span>
        <span className="dots">
          {items.map((x, i) => (
            <button
              key={i}
              className={`dot ${answerOf(saved, x) ? 'done' : ''} ${i === at ? 'here' : ''}`}
              title={x.question}
              onClick={() => go(i)}
            />
          ))}
        </span>
        <span className="quiet small push">
          {decided - toFindOut}/{items.length} decided{toFindOut ? ` · ${toFindOut} to find out` : ''}
        </span>
      </div>

      {q.kind === 'blocker' && (
        <div className="deck-kind">
          <OctagonAlert aria-hidden />
          Deploy blocker · who handles it?
        </div>
      )}
      {reopened && (
        <div className="deck-reopen">
          <RotateCcw aria-hidden /> Reopens your earlier decision: <Md text={reopened.question} />{' '}
          <CornerDownRight aria-hidden /> <em>{reopened.answer}</em>
        </div>
      )}
      <div className={`anchor ${q.anchor?.source === 'none' ? 'unanchored' : ''}`}>
        {q.anchor && q.anchor.source !== 'none' && q.anchor.quote ? (
          <>
            <span className="anchor-src">{anchorLabel(q.anchor)}</span>
            <blockquote>“{q.anchor.quote}”</blockquote>
          </>
        ) : q.anchor?.source === 'none' ? (
          <span className="anchor-src">Not in the ticket · Claude raised this because the code needs it</span>
        ) : null}
        <button
          className="link"
          onClick={() => {
            refine.track('show_ticket', issueKey, { index: q.index, anchor: q.anchor?.source ?? '' })
            onShowTicket(q.anchor ?? null)
          }}
        >
          <TextQuote aria-hidden />
          {q.anchor && q.anchor.source !== 'none' && q.anchor.quote ? 'Show in ticket' : 'Show the ticket'}
        </button>
      </div>
      {q.kind === 'blocker' ? (
        <BlockerText text={q.question} />
      ) : (
        <h2 className="deck-q">
          <Md text={q.question} />
        </h2>
      )}

      {options.length > 0 && (
        <div className="deck-options">
          {options.map((option, i) => (
            <button
              key={option}
              className={`deck-option ${answer === option ? 'picked' : ''}`}
              disabled={busy}
              onClick={() => void decide(option, 'option')}
            >
              <kbd>{i + 1}</kbd>
              <span>
                <span className="option-label">
                  <Md text={option} />
                </span>
                {q.consequences?.[i] && (
                  <span className="consequence">
                    <Md text={q.consequences[i]} />
                  </span>
                )}
              </span>
            </button>
          ))}
        </div>
      )}

      {words !== undefined ? (
        <div className="deck-words">
          <textarea
            autoFocus
            value={words}
            placeholder={options.length > 0 ? 'Your own answer' : 'Your answer, even just "no"'}
            onChange={(e) => setWords(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void decide(words.trim(), 'words')
              }
              if (e.key === 'Escape') setWords(undefined)
            }}
          />
          <div className="actions">
            <button className="primary" disabled={!words.trim()} onClick={() => void decide(words.trim(), 'words')}>
              Save answer
            </button>
            <button onClick={() => setWords(undefined)}>Cancel</button>
          </div>
        </div>
      ) : findOut !== undefined ? (
        <div className="deck-words">
          <textarea
            autoFocus
            value={findOut}
            placeholder="What do you need to find out, or from whom? (optional)"
            onChange={(e) => setFindOut(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void decide(UNKNOWN + findOut.trim(), 'unknown')
              }
              if (e.key === 'Escape') setFindOut(undefined)
            }}
          />
          <div className="actions">
            <button className="primary" onClick={() => void decide(UNKNOWN + findOut.trim(), 'unknown')}>
              I don't know yet
            </button>
            <button onClick={() => setFindOut(undefined)}>Cancel</button>
          </div>
        </div>
      ) : unknown !== null ? (
        <button className="deck-unknown" onClick={() => setFindOut(unknown)}>
          <CircleHelp aria-hidden /> <strong>Don't know yet.</strong> {unknown && <Md text={unknown} />}
          <span className="quiet small"> Claude tries to settle it next round, or says who can.</span>
        </button>
      ) : own ? (
        <button className="deck-own picked" onClick={() => setWords(answer)}>
          <CornerDownRight aria-hidden /> <Md text={answer} />
        </button>
      ) : (
        <div className="deck-alt">
          <button className="link deck-other" disabled={busy} onClick={() => setWords('')}>
            {options.length > 0 ? 'Answer in your own words…' : 'Write the answer…'}
          </button>
          <button className="link deck-other" disabled={busy} onClick={() => setFindOut('')}>
            <CircleHelp aria-hidden />
            I don't know yet
          </button>
        </div>
      )}

      {error && <div className="error-text small">{error}</div>}

      {evidence.length > 0 && (
        <details
          className="evidence"
          onToggle={(e) => {
            if ((e.target as HTMLDetailsElement).open) {
              opened.current = true
              refine.track('evidence_open', issueKey, { index: q.index, kind: q.kind, findings: evidence.length, arm: saved.draft.arm ?? '' })
            }
          }}
        >
          <summary>
            Why this matters · {evidence.length} finding{evidence.length === 1 ? '' : 's'}
          </summary>
          <ul className="bullets">
            {evidence.map((i) => (
              <li key={i}>
                <span className="marker">•</span>
                <span>
                  <Md text={findings[i]} />
                </span>
              </li>
            ))}
          </ul>
        </details>
      )}

      <div className="deck-nav">
        <button disabled={at === 0} onClick={() => go(at - 1)}>
          <ArrowLeft aria-hidden />
          Back
        </button>
        {decided < items.length ? (
          <button className="primary" disabled={!answer} onClick={nextOpen}>
            Next undecided
            <ArrowRight aria-hidden />
          </button>
        ) : (
          <>
            <button className="primary" disabled={busy} onClick={() => void refine.nextRound(issueKey)}>
              <RefreshCw aria-hidden />
              Next round with answers
            </button>
            <span className="quiet small">Claude reviews all {items.length} decisions and asks only what is still open.</span>
          </>
        )}
        {at < items.length - 1 && (
          <button className="link" onClick={() => go(at + 1)}>
            skip
          </button>
        )}
      </div>
    </section>
  )
}

function useKeys(handler: (e: KeyboardEvent) => void) {
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      if (target.closest('input, textarea') || e.metaKey || e.ctrlKey || e.altKey) return
      ref.current(e)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}

// record is every decision on a draft: the earlier rounds' log, then this
// round's answered questions, a question asked again keeping its latest
// answer, as draft.Decide does.
export function record(saved: Saved): Decided[] {
  const log = [...(saved.draft.decided ?? [])]
  for (const q of decisions(saved)) {
    const answer = answerOf(saved, q)
    if (!answer) continue
    const entry = { question: q.question, answer, recommended: q.recommended, why: q.why }
    const i = log.findIndex((e) => e.question === q.question)
    if (i >= 0) log[i] = entry
    else log.push(entry)
  }
  return log
}

// DecisionSummary lists every decision made over the rounds.
export function DecisionSummary({ saved }: { saved: Saved }) {
  const log = record(saved)
  const open = decisions(saved).filter((q) => !answerOf(saved, q))
  if (log.length === 0 && open.length === 0) return <div className="quiet italic">No decisions were needed.</div>
  return (
    <div className="summary-list">
      {log.map((e, i) => (
        <div key={i} className="summary-row">
          <div className="q-text">
            <Md text={e.question} />
          </div>
          <div className="summary-answer">
            ↳ <Md text={e.answer} />
          </div>
        </div>
      ))}
      {open.map((q, i) => (
        <div key={`open-${i}`} className="summary-row">
          <div className="q-text">
            <Md text={q.question} />
          </div>
          <div className="quiet italic small">still open: stamped without an answer</div>
        </div>
      ))}
    </div>
  )
}

// ClearCard is a questions round that came back with no questions: Claude
// has seen every answer, the developer sees every decision, and only then
// can the ticket be stamped.
export function ClearCard({ issueKey, saved, busy }: { issueKey: string; saved: Saved; busy: boolean }) {
  const refine = useRefine()
  const log = record(saved)
  return (
    <section className="deck clear">
      <div className="deck-head">
        <span className="deck-count">Questions are clear</span>
        <span className="quiet small push">
          {log.length} decision{log.length === 1 ? '' : 's'}
        </span>
      </div>
      <p>
        Claude reviewed your answers and has no more questions. Check the decisions below; the stamp writes the ticket
        from exactly these.
      </p>
      <DecisionSummary saved={saved} />
      <div className="deck-nav">
        <button
          className="primary"
          disabled={busy}
          onClick={() => {
            refine.track('stamp_from_clear', issueKey, { decisions: log.length, arm: saved.draft.arm ?? '' })
            void refine.stamp(issueKey)
          }}
        >
          Stamp
        </button>
        <span className="quiet small">Writes AC, Technical Implementation and points from these decisions.</span>
      </div>
    </section>
  )
}

// anchorLabel says where in the ticket a quote is.
function anchorLabel(a: Anchor): string {
  switch (a.source) {
    case 'description':
      return 'From the description'
    case 'criteria':
      return 'From the acceptance criteria in Jira'
    case 'comment':
      return a.ref ? `From ${a.ref}'s comment` : 'From a comment'
    case 'link':
      return a.ref ? `From linked ticket ${a.ref}` : 'From a linked ticket'
    case 'epic':
      return a.ref ? `From the epic, ${a.ref}` : 'From the epic'
    default:
      return 'From the ticket'
  }
}
