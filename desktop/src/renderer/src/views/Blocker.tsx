import { Users } from 'lucide-react'
import { Md } from './Md'

// Blocker is a deploy blocker read into its parts: what must be settled, why,
// and who can settle it. Claude writes it as one line ending in "Ask: <who>".
type Blocker = { lead: string; detail: string; who: string[] }

// identifier matches the code-like words a blocker names: env values
// (COMPOSER_AUTH), commands (queue:work), owner/repo paths and host or file
// names (starvault.tech).
const identifier = /(?<![`\w])([A-Z][A-Z0-9]*_[A-Z0-9_]+|[a-z]+:[a-z][\w-]*|[\w.-]+\/[\w./-]*[\w-]|[a-z][\w-]*\.(?:[a-z]{2,}|php|json|ya?ml|env|ts|js|vue)(?:\.[a-z]+)*)(?![`\w])/g

// coded wraps identifiers in backticks so Md shows them as code, leaving
// text that is already code alone.
export function coded(text: string): string {
  return text
    .split(/(`[^`]*`)/)
    .map((part) => (part.startsWith('`') ? part : part.replace(identifier, '`$1`')))
    .join('')
}

// owners splits "the org admin or DevOps (k8s and the base image)" into its
// people, never inside brackets.
function owners(list: string): string[] {
  const out: string[] = []
  let depth = 0
  let part = ''
  const words = list.split(/(\s+or\s+|\s+and\s+|,\s*|[()])/)
  for (const w of words) {
    if (w === '(') depth++
    if (w === ')') depth--
    if (depth === 0 && /^(\s+or\s+|\s+and\s+|,\s*)$/.test(w)) {
      out.push(part)
      part = ''
    } else part += w
  }
  out.push(part)
  return out.map((w) => w.trim().replace(/^the\s+/i, '')).filter(Boolean)
}

const sentence = (s: string) => {
  const t = s.trim().replace(/^[,;:\s]+/, '')
  if (!t) return ''
  const capped = t[0].toUpperCase() + t.slice(1)
  return /[.?!]$/.test(capped) ? capped : capped + '.'
}

export function readBlocker(text: string): Blocker {
  let body = text.trim()
  let who: string[] = []
  const ask = body.match(/\s*Ask:\s*(.+?)\.?\s*$/)
  if (ask && ask.index !== undefined) {
    body = body.slice(0, ask.index).trim()
    who = owners(ask[1])
  }
  // A second sentence, a colon or a reason clause starts the detail.
  const split =
    body.match(/^(.+?[.?!])\s+(\S.*)$/s) ??
    body.match(/^(.+?):\s+(\S.*)$/s) ??
    body.match(/^(.+?),\s+((?:since|because|so that|otherwise|or else)\s.*)$/is)
  if (!split) return { lead: sentence(body), detail: '', who }
  // A reason clause reads as its own sentence without its joining word.
  const detail = split[2].replace(/^(?:since|because)\s+/i, '')
  return { lead: sentence(split[1]), detail: sentence(detail), who }
}

// BlockerText shows a deploy blocker as a short ask, its reason, and who can
// answer it, instead of one run-on line.
export function BlockerText({ text }: { text: string }) {
  const b = readBlocker(text)
  return (
    <>
      <h2 className="deck-q">
        <Md text={coded(b.lead)} />
      </h2>
      {b.detail && (
        <p className="deck-detail">
          <Md text={coded(b.detail)} />
        </p>
      )}
      {b.who.length > 0 && (
        <div className="deck-who">
          <Users aria-hidden />
          <span className="quiet">Who can answer</span>
          {b.who.map((w) => (
            <span key={w} className="tag">
              <Md text={w} />
            </span>
          ))}
        </div>
      )}
    </>
  )
}
