import type { ReactNode } from 'react'
import {
  Ban,
  BookA,
  Check,
  CircleAlert,
  CircleDashed,
  CircleHelp,
  EyeOff,
  FileCode,
  Info,
  Lightbulb,
  Link2,
  ListTodo,
  MessageCircleQuestion,
  OctagonAlert,
  Target,
  TriangleAlert,
  Workflow,
  type LucideIcon
} from 'lucide-react'
import type { Brief, ItemStatus, Relation, ScopeQuestion, Triage } from '../types'
import { Md } from './Md'

export function Card({
  title,
  children,
  tone,
  icon: Icon
}: {
  title: ReactNode
  children: ReactNode
  tone?: 'warn'
  icon?: LucideIcon
}) {
  return (
    <section className={`card ${tone ?? ''}`}>
      <h3>
        {Icon && <Icon aria-hidden />}
        {title}
      </h3>
      {children}
    </section>
  )
}

export function Bullets({ items, marker = '•' }: { items: string[] | null | undefined; marker?: ReactNode }) {
  if (!items || items.length === 0) return <div className="quiet italic">none</div>
  return (
    <ul className="bullets">
      {items.map((item, i) => (
        <li key={i}>
          <span className="marker">{marker}</span>
          <span>
            <Md text={item} />
          </span>
        </li>
      ))}
    </ul>
  )
}

const relationLabel: Record<string, string> = {
  needs: 'needs',
  needed_by: 'needed by',
  duplicates: 'duplicates',
  conflicts: 'conflicts with',
  overlaps: 'overlaps'
}

export function Relations({ relations }: { relations: Relation[] }) {
  return (
    <div className="relations">
      {relations.map((r, i) => (
        <div key={i}>
          <span className="key">
            {relationLabel[r.kind] ?? r.kind} {r.key}
          </span>
          <div className="indent">
            <Md text={r.why} />
          </div>
        </div>
      ))}
    </div>
  )
}

// TriageCards shows a triage; withScope false leaves out the scope questions,
// for the approach screen, which answers them beside it.
export function TriageCards({ triage, withScope = true }: { triage: Triage; withScope?: boolean }) {
  return (
    <div className="stack narrow">
      <Card title="Problem" icon={CircleAlert}>
        <p>
          <Md text={triage.problem} />
        </p>
      </Card>
      <Card title="Solution" icon={Target}>
        <p>
          <Md text={triage.solution} />
        </p>
      </Card>
      {triage.items && triage.items.length > 0 && (
        <Card title="Items" icon={ListTodo}>
          <ul className="bullets">
            {triage.items.map((item, i) => (
              <li key={i}>
                <span className="marker">•</span>
                <span>
                  <Md text={item.item} />
                  <span className="quiet small"> “{item.quote}”</span>
                </span>
              </li>
            ))}
          </ul>
        </Card>
      )}
      <Card title="Blockers" icon={OctagonAlert}>
        <Bullets items={triage.blockers} marker={<OctagonAlert className="tone-err" aria-hidden />} />
      </Card>
      {withScope && (
        <Card title="Scope questions" icon={CircleHelp}>
          <ScopeList questions={triage.scope_questions} />
        </Card>
      )}
      {triage.relations && triage.relations.length > 0 && (
        <Card title="Related tickets" icon={Link2}>
          <Relations relations={triage.relations} />
        </Card>
      )}
    </div>
  )
}

// ScopeList lists the scope questions with their options. Claude's pick is
// left out: the developer decides on the approach screen first.
function ScopeList({ questions }: { questions: ScopeQuestion[] | null }) {
  if (!questions || questions.length === 0) return <div className="quiet italic">none</div>
  return (
    <div className="scope-list">
      {questions.map((q) => (
        <div key={q.question} className="scope-q">
          <div className="scope-ask">
            <Md text={q.question} />
          </div>
          {(q.options ?? []).map((option, i) => (
            <div key={option} className="scope-option">
              •{' '}
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
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

const statusLabel: Record<ItemStatus['status'], [string, LucideIcon]> = {
  needed: ['needed', CircleDashed],
  done: ['done', Check],
  looks_unused: ['looks unused?', EyeOff],
  ask_team: ['ask team', MessageCircleQuestion],
  blocked: ['blocked', Ban]
}

function StatusTag({ status }: { status: ItemStatus['status'] }) {
  const [label, Icon] = statusLabel[status] ?? [status, CircleDashed]
  return (
    <span className={`tag status-${status}`}>
      <Icon aria-hidden />
      {label}
    </span>
  )
}

// ItemStatuses lists every item the ticket asks for with what the code shows,
// so nothing the ticket asks for drops out unseen.
function ItemStatuses({ items }: { items: ItemStatus[] }) {
  return (
    <div className="item-statuses">
      {items.map((item, i) => (
        <div key={i} className="item-status">
          <StatusTag status={item.status} />
          <span>
            <Md text={item.item} />
            {item.note && (
              <span className="consequence">
                <Md text={item.note} />
              </span>
            )}
            {item.evidence && <span className="mono quiet small">{item.evidence}</span>}
          </span>
        </div>
      ))}
    </div>
  )
}

export function BriefCards({ brief }: { brief: Brief }) {
  let repo = ''
  return (
    <div className="stack">
      {brief.items && brief.items.length > 0 && (
        <Card title="What the ticket asks for" icon={ListTodo}>
          <ItemStatuses items={brief.items} />
        </Card>
      )}
      {brief.claims && brief.claims.length > 0 && (
        <Card title="Ticket claims the code contradicts" icon={TriangleAlert} tone="warn">
          {brief.claims.map((c, i) => (
            <div key={i} className="claim">
              <div className="quiet">“{c.claim}”</div>
              <div className="indent">
                <Md text={c.finding} />
              </div>
              {c.evidence && <div className="indent mono quiet small">{c.evidence}</div>}
            </div>
          ))}
        </Card>
      )}
      <Card title="What this area does" icon={Info}>
        <p>
          <Md text={brief.summary} />
        </p>
      </Card>
      <Card title="How it flows today" icon={Workflow}>
        {brief.flow && brief.flow.length > 0 ? (
          <ol className="flow">
            {brief.flow.map((step, i) => (
              <li key={i}>
                <Md text={step} />
              </li>
            ))}
          </ol>
        ) : (
          <div className="quiet italic">none</div>
        )}
      </Card>
      <Card title="Key files" icon={FileCode}>
        {brief.parts && brief.parts.length > 0 ? (
          brief.parts.map((part, i) => {
            const head = part.repo !== repo
            repo = part.repo
            return (
              <div key={i}>
                {head && <div className="key">{part.repo}</div>}
                <div className="indent mono">{part.path}</div>
                <div className="indent2 quiet">
                  <Md text={part.role} />
                </div>
              </div>
            )
          })
        ) : (
          <div className="quiet italic">none</div>
        )}
      </Card>
      <Card title="Terms" icon={BookA}>
        {brief.terms && brief.terms.length > 0 ? (
          <ul className="bullets">
            {brief.terms.map((term, i) => (
              <li key={i}>
                <span className="marker">•</span>
                <span>
                  <strong>{term.term}</strong>: <Md text={term.meaning} />
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <div className="quiet italic">none</div>
        )}
      </Card>
      <Card title="Gotchas" icon={Lightbulb}>
        <Bullets items={brief.gotchas} />
      </Card>
      <Card title="Ask the team" icon={MessageCircleQuestion}>
        <Bullets items={brief.ask_team} marker={<CircleHelp aria-hidden />} />
      </Card>
    </div>
  )
}
