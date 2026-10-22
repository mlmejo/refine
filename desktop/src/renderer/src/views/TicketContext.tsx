import { useEffect, type ReactNode } from 'react'
import { useRefine } from '../store'
import type { Anchor } from '../types'
import { Highlight, MdDoc } from './MdDoc'

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="card ctx">
      <h3>{title}</h3>
      {children}
    </section>
  )
}

// TicketContext shows the ticket as Jira has it, with the anchor's quote
// marked: the description, comments, links, and the epic with its other
// tickets. Nothing here is summarised.
export function TicketContext({ issueKey, anchor }: { issueKey: string; anchor?: Anchor | null }) {
  const refine = useRefine()
  const t = refine.ticket(issueKey)
  useEffect(() => {
    if (!t.context && !t.contextError) void refine.loadContext(issueKey)
  }, [issueKey, t.context, t.contextError, refine])
  const ctx = t.context
  if (t.contextError) return <div className="error-text">{t.contextError}</div>
  if (!ctx) {
    return (
      <div className="quiet">
        <span className="spinner" /> Reading the ticket…
      </div>
    )
  }
  const quote = anchor?.quote
  const site = refine.info?.site
  const url = (key: string) => (site ? `${site.includes('://') ? site : `https://${site}`}/browse/${key}` : undefined)
  return (
    <div className="stack">
      <Section title="Description">
        {ctx.description_md ? (
          <MdDoc text={ctx.description_md} quote={anchor?.source === 'description' ? quote : undefined} />
        ) : ctx.description ? (
          <div className="prewrap">
            <Highlight text={ctx.description} quote={anchor?.source === 'description' ? quote : undefined} />
          </div>
        ) : (
          <div className="quiet italic">(empty)</div>
        )}
      </Section>
      {ctx.acceptance_criteria && (
        <Section title="Acceptance Criteria in Jira">
          <div className="prewrap">
            <Highlight text={ctx.acceptance_criteria} quote={anchor?.source === 'criteria' ? quote : undefined} />
          </div>
        </Section>
      )}
      <Section title={`Comments · ${ctx.comments.length}`}>
        {ctx.comments.length === 0 && <div className="quiet italic">none</div>}
        {ctx.comments.map((c, i) => (
          <div key={i} className="ctx-comment">
            <div className="small quiet">{c.author}</div>
            <div className="prewrap">
              <Highlight text={c.body} quote={anchor?.source === 'comment' ? quote : undefined} />
            </div>
          </div>
        ))}
      </Section>
      {ctx.links.length > 0 && (
        <Section title={`Linked tickets · ${ctx.links.length}`}>
          {ctx.links.map((l, i) => (
            <div key={i} className={`ctx-link ${anchor?.source === 'link' && anchor.ref === l.key ? 'marked' : ''}`}>
              <span className="quiet small">{l.relation}</span>{' '}
              <a className="key" href={url(l.key)} target="_blank" rel="noreferrer">
                {l.key}
              </a>{' '}
              {l.summary} <span className="tag">{l.status}</span>
            </div>
          ))}
        </Section>
      )}
      {ctx.parent && (
        <Section title="Epic">
          <div className={`ctx-link ${anchor?.source === 'epic' ? 'marked' : ''}`}>
            <a className="key" href={url(ctx.parent.key)} target="_blank" rel="noreferrer">
              {ctx.parent.key}
            </a>{' '}
            {ctx.parent.summary}
          </div>
          {ctx.siblings.length > 0 && (
            <>
              <h4>Its other tickets · {ctx.siblings.length}</h4>
              {ctx.siblings.map((s) => (
                <div key={s.key} className={`ctx-link ${anchor?.source === 'epic' && anchor.ref === s.key ? 'marked' : ''}`}>
                  <a className="key" href={url(s.key)} target="_blank" rel="noreferrer">
                    {s.key}
                  </a>{' '}
                  {s.summary} {s.status && <span className="tag">{s.status}</span>}
                </div>
              ))}
            </>
          )}
        </Section>
      )}
    </div>
  )
}
