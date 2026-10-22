import { createContext, useContext, useMemo, useRef, type ReactNode } from 'react'
import type { Term } from '../types'

// pattern matches the inline markdown Claude writes: `code`, **bold** and
// [text](https://link). An unmatched marker stays as it is.
const pattern = /`([^`\n]+)`|\*\*([^*\n]+)\*\*|\[([^\]\n]+)\]\((https:\/\/[^)\s]+)\)/g

// citation matches code that is a file reference, e.g.
// dating/components/navbar.vue:169 or Repo.php:16.
const citation = /^[\w@.~-]*(\/[\w@.-]+)*\.\w+(:[\d,\s-]+)?$/

// Chip shows a file reference as its file name and lines, the full path on
// hover, so paths stop dominating the text.
function Chip({ path }: { path: string }) {
  const short = path.split('/').pop() ?? path
  return (
    <code className="cite" title={path}>
      {short}
    </code>
  )
}

// Terms carries arm B's plain names into every Md under it, with a hook to
// record a hover. Outside a B draft it is empty and Md renders as before.
type TermsValue = { terms: Term[]; onHover?: (name: string) => void }
const TermsContext = createContext<TermsValue>({ terms: [] })

export function TermsProvider({ terms, onHover, children }: TermsValue & { children: ReactNode }) {
  const seen = useRef(new Set<string>())
  const value = useMemo(
    () => ({
      terms,
      // Record each term's first hover only: whether it was needed, not how
      // often the mouse crossed it.
      onHover: (name: string) => {
        if (seen.current.has(name)) return
        seen.current.add(name)
        onHover?.(name)
      }
    }),
    [terms, onHover]
  )
  return <TermsContext.Provider value={value}>{children}</TermsContext.Provider>
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// withTerms wraps each plain name in text with its code on hover.
function withTerms(text: string, { terms, onHover }: TermsValue, key: string): ReactNode[] {
  const named = terms.filter((t) => t.name.trim()).sort((a, b) => b.name.length - a.name.length)
  if (named.length === 0) return [text]
  const byName = new Map(named.map((t) => [t.name.toLowerCase(), t]))
  const re = new RegExp(`(${named.map((t) => escape(t.name)).join('|')})`, 'gi')
  return text.split(re).map((part, i) => {
    const term = byName.get(part.toLowerCase())
    if (!term) return part
    const title = [...(term.code ?? []), ...(term.where ?? [])].join('\n')
    return (
      <span key={`${key}-${i}`} className="term" title={title} onMouseEnter={() => onHover?.(term.name)}>
        {part}
      </span>
    )
  })
}

// Md renders text's inline markdown, building elements rather than HTML so
// nothing in the text can inject markup. File references become chips, and
// the parentheses Claude wraps them in are dropped.
export function Md({ text }: { text: string | null | undefined }) {
  const terms = useContext(TermsContext)
  if (!text) return null
  const out: ReactNode[] = []
  let last = 0
  const push = (s: string) => {
    if (s) out.push(...withTerms(s, terms, String(out.length)))
  }
  for (const match of text.matchAll(pattern)) {
    const at = match.index ?? 0
    const [, code, bold, label, url] = match
    let before = text.slice(last, at)
    let end = at + match[0].length
    if (code !== undefined && citation.test(code.trim())) {
      // "( `a.vue:1`, `b.vue:2` )" reads as chips without the brackets.
      before = before.replace(/\s*\(\s*$/, ' ')
      push(before)
      out.push(<Chip key={at} path={code.trim()} />)
      const close = text.slice(end).match(/^\s*\)/)
      if (close) end += close[0].length
    } else if (code !== undefined) {
      push(before)
      out.push(<code key={at}>{code}</code>)
    } else if (bold !== undefined) {
      push(before)
      out.push(<strong key={at}>{bold}</strong>)
    } else {
      push(before)
      out.push(
        <a key={at} href={url} target="_blank" rel="noreferrer">
          {label}
        </a>
      )
    }
    last = end
  }
  push(text.slice(last))
  return <>{out}</>
}
