import { useEffect, useState } from 'react'
import { useRefine } from '../store'
import type { Step, View } from '../types'
import { Start } from './steps/Start'
import { Related } from './steps/Related'
import { Repos } from './steps/Repos'
import { Questions } from './steps/Questions'
import { Deploy } from './steps/Deploy'
import { Results } from './steps/Results'
import { Stamped } from './steps/Stamped'
import { Working } from './Working'
import { typing } from '../typing'

const steps: { step: Step; label: string }[] = [
  { step: 'related', label: 'Related' },
  { step: 'repos', label: 'Repos' },
  { step: 'questions', label: 'Questions' },
  { step: 'deploy', label: 'Deploy' },
  { step: 'results', label: 'Results' },
  { step: 'stamped', label: 'Stamp' }
]

const order = (s: Step) => steps.findIndex((x) => x.step === s)

function StepBar({ view, viewing, onView }: { view: View; viewing: Step; onView: (s: Step) => void }) {
  const current = view.started ? order(view.refinement.step) : -1
  return (
    <ol className="steps" aria-label="Steps">
      {steps.map(({ step, label }, i) => {
        const done = i < current || view.refinement.step === 'stamped'
        const state = done ? 'done' : i === current ? 'current' : 'todo'
        return (
          <li key={step} className="row" style={{ gap: 2 }}>
            {i > 0 && (
              <span className="sep" aria-hidden="true">
                ›
              </span>
            )}
            <button
              className={`step-pill ${state} ${viewing === step && done ? 'viewing' : ''}`}
              disabled={state === 'todo'}
              aria-current={state === 'current' ? 'step' : undefined}
              onClick={() => onView(step)}
            >
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                {state === 'done' && (
                  <>
                    <circle cx="6" cy="6" r="5.25" fill="var(--green)" />
                    <path d="M3.6 6.2 5.2 7.8 8.4 4.6" fill="none" stroke="var(--bg)" strokeWidth="1.5" />
                  </>
                )}
                {state === 'current' && (
                  <>
                    <circle cx="6" cy="6" r="4.75" fill="none" stroke="var(--accent)" strokeWidth="1.5" />
                    <circle cx="6" cy="6" r="2" fill="var(--accent)" />
                  </>
                )}
                {state === 'todo' && (
                  <circle cx="6" cy="6" r="4.75" fill="none" stroke="var(--border-strong)" strokeWidth="1.5" strokeDasharray="2 2" />
                )}
              </svg>
              {label}
            </button>
          </li>
        )
      })}
    </ol>
  )
}

// Ticket is one ticket's refinement: its header and step bar, then the step
// it is at, or a finished step looked back at.
export function Ticket({ ticketKey }: { ticketKey: string }) {
  const refine = useRefine()
  const view = refine.views[ticketKey]
  const error = refine.errors[ticketKey]
  const running = refine.job && !refine.job.done && refine.job.key === ticketKey ? refine.job : undefined
  const [viewing, setViewing] = useState<Step>()
  const step = view?.refinement.step

  // Moving on shows the new step.
  useEffect(() => setViewing(undefined), [step])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !refine.palette && !typing(e)) {
        refine.setScreen({ kind: 'tickets' })
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [refine])

  const shown: Step | undefined = viewing ?? step
  const readOnly = viewing !== undefined && viewing !== step

  return (
    <>
      <header className="ticket-head">
        <div className="crumbs">
          <button className="link" style={{ color: 'var(--muted)', fontSize: 13 }} onClick={() => refine.setScreen({ kind: 'tickets' })}>
            Tickets
          </button>
          <span className="sep" aria-hidden="true">
            ›
          </span>
          <span className="mono muted">{ticketKey}</span>
          <span className="title ellipsis">{view?.ticket.summary || view?.refinement.summary}</span>
          {refine.info?.site && (
            <a
              href={`https://${refine.info.site}/browse/${ticketKey}`}
              target="_blank"
              rel="noreferrer"
              className="small"
              style={{ marginLeft: 'auto', whiteSpace: 'nowrap' }}
            >
              Open in Jira ↗
            </a>
          )}
        </div>
        {view && <StepBar view={view} viewing={shown ?? 'related'} onView={(s) => setViewing(s === step ? undefined : s)} />}
      </header>
      {error && !running && (
        <div style={{ padding: '12px 20px 0' }}>
          <div className="banner error">{error}</div>
        </div>
      )}
      {!view && !error && (
        <div className="empty row" style={{ justifyContent: 'center' }}>
          <span className="spinner" /> Reading {ticketKey} from Jira…
        </div>
      )}
      {view && running && <Working job={running} />}
      {view && !running && (
        <>
          {!view.started && <Start view={view} />}
          {view.started && shown === 'related' && <Related view={view} readOnly={readOnly} />}
          {view.started && shown === 'repos' && <Repos view={view} readOnly={readOnly} />}
          {view.started && shown === 'questions' && <Questions key={view.refinement.rounds?.length ?? 0} view={view} readOnly={readOnly} />}
          {view.started && shown === 'deploy' && <Deploy key={view.refinement.deploy ? 'asked' : 'not yet'} view={view} readOnly={readOnly} />}
          {view.started && shown === 'results' && <Results view={view} readOnly={readOnly} />}
          {view.started && shown === 'stamped' && <Stamped view={view} />}
        </>
      )}
    </>
  )
}
