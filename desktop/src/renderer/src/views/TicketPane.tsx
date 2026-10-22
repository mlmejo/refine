import { useEffect, useState } from 'react'
import { BookOpen, Check, FilePen, FileText, ListChecks, PenLine, RotateCcw, Stamp, X, type LucideIcon } from 'lucide-react'
import { useRefine, type Tab } from '../store'
import type { Issue, Job } from '../types'
import { TriageCards, BriefCards } from './Cards'
import { Approach } from './Approach'
import { DraftView } from './DraftView'
import { TicketContext } from './TicketContext'

export function TicketPane({ issueKey }: { issueKey: string }) {
  const refine = useRefine()
  const issue = refine.issues.find((i) => i.key === issueKey)
  const t = refine.ticket(issueKey)
  if (!issue) return null
  const saved = t.saved
  const running = t.job && !t.job.done ? t.job : undefined

  const tabs: { id: Tab; label: string; icon: LucideIcon; done?: boolean; show: boolean }[] = [
    { id: 'ticket', label: 'Ticket', icon: FileText, show: true },
    { id: 'triage', label: 'Triage', icon: ListChecks, done: !!saved?.triage, show: true },
    { id: 'brief', label: 'Brief', icon: BookOpen, done: !!saved?.brief, show: true },
    { id: 'approach', label: 'Approach', icon: PenLine, show: true },
    {
      id: 'draft',
      label: saved?.draft?.step === 'stamp' ? 'Draft · stamped' : 'Draft',
      icon: saved?.draft?.step === 'stamp' ? Stamp : FilePen,
      show: !!saved?.draft
    }
  ]
  const site = refine.info?.site
  const jiraURL = site ? `${site.includes('://') ? site : `https://${site}`}/browse/${issue.key}` : undefined

  return (
    <div className="pane">
      <header className="pane-head">
        <div className="pane-title">
          <span className="key big">{issue.key}</span>
          <span className="summary">{issue.summary}</span>
        </div>
        <div className="pane-meta quiet small">
          {issue.status}
          {jiraURL && (
            <>
              {' · '}
              <a href={jiraURL} target="_blank" rel="noreferrer">
                open in Jira
              </a>
            </>
          )}
        </div>
        <nav className="tabs">
          {tabs
            .filter((tab) => tab.show)
            .map((tab) => (
              <button
                key={tab.id}
                className={`tab ${t.tab === tab.id ? 'active' : ''}`}
                onClick={() => refine.setTab(issue.key, tab.id)}
              >
                <tab.icon aria-hidden />
                {tab.label}
                {tab.done && <Check className="tab-done" aria-label="done" />}
              </button>
            ))}
        </nav>
      </header>

      {running && <RunBanner job={running} notes={t.notes} onCancel={() => void refine.cancel(issue.key)} />}
      {refine.checking === issue.key && (
        <div className="banner">
          <span className="spinner" /> Checking the repos are up to date with origin…
        </div>
      )}
      {t.error && <div className="banner error">{t.error}</div>}

      <div className="pane-body">
        {!saved ? (
          <div className="quiet pad">
            <span className="spinner" /> Loading…
          </div>
        ) : (
          <TabBody issue={issue} tab={t.tab} busy={!!running} />
        )}
      </div>
    </div>
  )
}

function TabBody({ issue, tab, busy }: { issue: Issue; tab: Tab; busy: boolean }) {
  const refine = useRefine()
  const saved = refine.ticket(issue.key).saved!
  const key = issue.key

  switch (tab) {
    case 'ticket':
      return (
        <div className="stack">
          <Field name="Acceptance Criteria" text={issue.acceptance_criteria} />
          <Field name="Technical Implementation" text={issue.technical_implementation} />
          <Field name="Story Point Criteria" text={issue.story_point_criteria} />
          <TicketContext issueKey={key} />
          <div className="actions">
            <button disabled={busy} onClick={() => (saved.triage ? refine.setTab(key, 'triage') : void refine.triage(key))}>
              <ListChecks aria-hidden />
              {saved.triage ? 'See triage' : 'Triage'}
            </button>
            <button disabled={busy} onClick={() => (saved.brief ? refine.setTab(key, 'brief') : void refine.brief(key))}>
              <BookOpen aria-hidden />
              {saved.brief ? 'See brief' : 'Brief me'}
            </button>
            <button className="primary" onClick={() => refine.setTab(key, 'approach')}>
              <PenLine aria-hidden />
              {saved.draft ? 'Redo approach' : 'Approach & questions'}
            </button>
            {saved.draft && (
              <button onClick={() => refine.setTab(key, 'draft')}>
                <FileText aria-hidden />
                Saved draft
              </button>
            )}
          </div>
        </div>
      )

    case 'triage':
      if (!saved.triage) {
        return (
          <Empty
            text="Triage reads the ticket from Jira alone, with no code: problem, solution, blockers, scope questions and related tickets. Quick and cheap."
            action="Triage"
            icon={ListChecks}
            busy={busy}
            onAction={() => void refine.triage(key)}
            alt="Approach without a triage"
            onAlt={() => refine.setTab(key, 'approach')}
          />
        )
      }
      return (
        <div className="stack">
          <RunMeta run={saved.triage} />
          <TriageCards triage={saved.triage.triage} />
          <div className="actions">
            <button className="primary" onClick={() => refine.setTab(key, 'approach')}>
              <PenLine aria-hidden />
              Write your approach
            </button>
            <button disabled={busy} onClick={() => (saved.brief ? refine.setTab(key, 'brief') : void refine.brief(key))}>
              <BookOpen aria-hidden />
              {saved.brief ? 'See brief' : 'Brief me'}
            </button>
            <button disabled={busy} onClick={() => void refine.triage(key)}>
              <RotateCcw aria-hidden />
              Triage again
            </button>
          </div>
        </div>
      )

    case 'brief':
      if (!saved.brief) {
        return (
          <Empty
            text="A brief reads the code this ticket touches and explains the area before you write your approach. A draft afterwards continues its session, so it rereads little code."
            action="Brief me"
            icon={BookOpen}
            busy={busy}
            onAction={() => void refine.brief(key)}
            alt="Approach without a brief"
            onAlt={() => refine.setTab(key, 'approach')}
          />
        )
      }
      return (
        <div className="stack">
          <RunMeta run={saved.brief} extra={saved.brief.ref ? `repos at ${saved.brief.ref}` : undefined} />
          <BriefCards brief={saved.brief.brief} />
          <div className="actions">
            <button className="primary" onClick={() => refine.setTab(key, 'approach')}>
              <PenLine aria-hidden />
              Write your approach
            </button>
            <button disabled={busy} onClick={() => void refine.brief(key)}>
              <RotateCcw aria-hidden />
              Brief again
            </button>
          </div>
        </div>
      )

    case 'approach':
      return <Approach issueKey={key} busy={busy} />

    case 'draft':
      return saved.draft ? <DraftView issueKey={key} busy={busy} /> : null
  }
}

function Field({ name, text }: { name: string; text: string }) {
  return (
    <section className="card">
      <h3>{name}</h3>
      {text ? <div className="prewrap">{text}</div> : <div className="quiet">(empty)</div>}
    </section>
  )
}

function Empty(props: {
  text: string
  action: string
  icon: LucideIcon
  busy: boolean
  onAction: () => void
  alt: string
  onAlt: () => void
}) {
  return (
    <div className="empty-step">
      <p>{props.text}</p>
      <div className="actions">
        <button className="primary" disabled={props.busy} onClick={props.onAction}>
          <props.icon aria-hidden />
          {props.action}
        </button>
        <button onClick={props.onAlt}>{props.alt}</button>
      </div>
    </div>
  )
}

export function RunMeta({ run, extra }: { run: { cost_usd: number; created: string; turns: number }; extra?: string }) {
  const parts = [`$${run.cost_usd.toFixed(2)}`, new Date(run.created).toLocaleString()]
  if (run.turns) parts.push(`${run.turns} turns`)
  if (extra) parts.push(extra)
  return <div className="quiet small">{parts.join(' · ')}</div>
}

const runText: Record<string, [string, string]> = {
  triage: ['Claude is triaging the ticket from Jira…', 'No code, so usually well under a minute.'],
  brief: ['Claude is reading the repos for a brief…', 'Usually a few minutes, on your Claude plan.'],
  draft: [
    "Claude is reading the repos for its questions…",
    'Usually about 2 minutes, on your Claude plan. Triages the ticket first if it has no triage.'
  ],
  fromBrief: ['Claude is drafting from the brief…', "Continues the brief's session, so it rereads little code."],
  resume: ['Claude is working through your answers…', 'Picks up the earlier session, so it rereads little code.'],
  stamp: [
    'Claude is stamping the ticket from your answers…',
    'Writes Acceptance Criteria, Technical Implementation, points and the split, then checks them against the other open tickets. Forks the questions session.'
  ]
}

function RunBanner({ job, notes, onCancel }: { job: Job; notes: string[]; onCancel: () => void }) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  const elapsed = Math.max(0, Math.floor((now - new Date(job.started).getTime()) / 1000))
  const which = job.kind === 'draft' ? (job.resume ? 'resume' : job.from_brief ? 'fromBrief' : 'draft') : job.kind
  const [title, hint] = runText[which]
  const clock = `${Math.floor(elapsed / 60)}m ${String(elapsed % 60).padStart(2, '0')}s`
  return (
    <div className="banner running">
      <div className="banner-row">
        <span className="spinner" />
        <strong>{title}</strong>
        <span className="quiet">{clock}</span>
        <button className="push" onClick={onCancel}>
          <X aria-hidden />
          Cancel
        </button>
      </div>
      <div className="quiet small">{hint}</div>
      {notes.map((note) => (
        <div key={note} className="quiet small">
          {note}
        </div>
      ))}
    </div>
  )
}
