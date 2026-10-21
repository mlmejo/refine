import type { ReactNode } from 'react'
import type { Brief, Relation, ScopeQuestion, Triage } from '../types'
import { Md } from './Md'

export function Card({ title, children, tone }: { title: ReactNode; children: ReactNode; tone?: 'warn' }) {
  return (
    <section className={`card ${tone ?? ''}`}>
      <h3>{title}</h3>
      {children}
    </section>
  )
}

export function Bullets({ items, marker = '•' }: { items: string[] | null | undefined; marker?: string }) {
  if (!items || items.length === 0) return <div className="quiet italic">none</div>
  return (
    <ul className="bullets" data-marker={marker}>
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
      <Card title="Problem">
        <p>
          <Md text={triage.problem} />
        </p>
      </Card>
      <Card title="Solution">
        <p>
          <Md text={triage.solution} />
        </p>
      </Card>
      <Card title="Blockers">
        <Bullets items={triage.blockers} marker="⛔" />
      </Card>
      {withScope && (
        <Card title="Scope questions">
          <ScopeList questions={triage.scope_questions} />
        </Card>
      )}
      {triage.relations && triage.relations.length > 0 && (
        <Card title="Related tickets">
          <Relations relations={triage.relations} />
        </Card>
      )}
    </div>
  )
}

// ScopeList lists the scope questions with their options, the recommended
// one starred with its reason.
function ScopeList({ questions }: { questions: ScopeQuestion[] | null }) {
  if (!questions || questions.length === 0) return <div className="quiet italic">none</div>
  return (
    <div className="scope-list">
      {questions.map((q) => (
        <div key={q.question} className="scope-q">
          <div className="scope-ask">
            <Md text={q.question} />
          </div>
          {(q.options ?? []).map((option) => (
            <div key={option} className="scope-option">
              • <Md text={option} />
              {option === q.recommended && <span className="star"> ★ recommended</span>}
            </div>
          ))}
          <div className="quiet small">
            ★ <Md text={(q.options ?? []).length === 0 ? [q.recommended, q.why].filter(Boolean).join(', ') : q.why} />
          </div>
        </div>
      ))}
    </div>
  )
}

export function BriefCards({ brief }: { brief: Brief }) {
  let repo = ''
  return (
    <div className="stack">
      <Card title="What this area does">
        <p>
          <Md text={brief.summary} />
        </p>
      </Card>
      <Card title="How it flows today">
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
      <Card title="Key files">
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
      <Card title="Terms">
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
      <Card title="Gotchas">
        <Bullets items={brief.gotchas} />
      </Card>
      <Card title="Ask the team">
        <Bullets items={brief.ask_team} marker="?" />
      </Card>
    </div>
  )
}
