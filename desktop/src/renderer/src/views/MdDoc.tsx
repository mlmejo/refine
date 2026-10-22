import { useEffect, useRef, type ReactNode } from 'react'

// fold lowercases text and collapses runs of space to one, keeping where
// each folded character came from, so a quote finds its place in the
// ticket even when Claude's spacing differs.
export function fold(text: string): { folded: string; from: number[] } {
  let folded = ''
  const from: number[] = []
  for (let i = 0; i < text.length; i++) {
    const space = /\s/.test(text[i])
    if (space && folded.endsWith(' ')) continue
    folded += space ? ' ' : text[i].toLowerCase()
    from.push(i)
  }
  return { folded, from }
}

// Highlight shows text with the quote marked, when it is in it, and
// scrolls to the mark.
export function Highlight({ text, quote }: { text: string; quote?: string }) {
  const ref = useRef<HTMLElement>(null)
  useEffect(() => ref.current?.scrollIntoView({ block: 'center', behavior: 'smooth' }), [quote])
  const q = quote ? fold(quote.trim()).folded : ''
  if (!q) return <>{text}</>
  const { folded, from } = fold(text)
  const at = folded.indexOf(q)
  if (at < 0) return <>{text}</>
  const start = from[at]
  const end = from[at + q.length - 1] + 1
  return (
    <>
      {text.slice(0, start)}
      <mark ref={ref}>{text.slice(start, end)}</mark>
      {text.slice(end)}
    </>
  )
}

// inline matches the inline markdown Jira's formatting becomes: `code`,
// **bold**, ~~strike~~, *italics*, [text](link) and bare links.
const inline =
  /`([^`\n]+)`|\*\*([^*\n]+)\*\*|~~([^~\n]+)~~|\*([^*\s][^*\n]*)\*|\[([^\]\n]+)\]\((https?:\/\/[^)\s]+)\)|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/g

// plain is the text a reader sees, with the markers dropped, so a quote can
// be found in it.
const plain = (text: string) =>
  text.replace(inline, (_m, code, bold, strike, em, label, _url, bare) => code ?? bold ?? strike ?? em ?? label ?? bare)

function Inline({ text }: { text: string }) {
  const out: ReactNode[] = []
  let last = 0
  for (const match of text.matchAll(inline)) {
    const at = match.index ?? 0
    const [, code, bold, strike, em, label, url, bare] = match
    out.push(text.slice(last, at))
    if (code !== undefined) out.push(<code key={at}>{code}</code>)
    else if (bold !== undefined) out.push(<strong key={at}>{bold}</strong>)
    else if (strike !== undefined) out.push(<s key={at}>{strike}</s>)
    else if (em !== undefined) out.push(<em key={at}>{em}</em>)
    else
      out.push(
        <a key={at} href={url ?? bare} target="_blank" rel="noreferrer">
          {label ?? bare}
        </a>
      )
    last = at + match[0].length
  }
  out.push(text.slice(last))
  return <>{out}</>
}

// Text renders a line's inline markdown, or, when it holds the quote, its
// plain text with the quote marked.
function Text({ text, quote }: { text: string; quote?: string }) {
  const q = quote ? fold(quote.trim()).folded : ''
  if (q && fold(plain(text)).folded.includes(q)) return <Highlight text={plain(text)} quote={quote} />
  return <Inline text={text} />
}

type Block =
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'rule' }
  | { kind: 'list'; ordered: boolean; items: string[] }
  | { kind: 'quote'; text: string }
  | { kind: 'code'; text: string }
  | { kind: 'para'; text: string }

// blocks splits markdown into the blocks the ticket's description uses.
function blocks(md: string): Block[] {
  const out: Block[] = []
  const lines = md.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (!line.trim()) continue
    const heading = line.match(/^(#{1,6})\s+(.*)$/)
    if (heading) {
      out.push({ kind: 'heading', level: heading[1].length, text: heading[2] })
    } else if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
      out.push({ kind: 'rule' })
    } else if (line.startsWith('```')) {
      const code: string[] = []
      while (++i < lines.length && !lines[i].startsWith('```')) code.push(lines[i])
      out.push({ kind: 'code', text: code.join('\n') })
    } else if (line.startsWith('>')) {
      const quote: string[] = []
      for (; i < lines.length && lines[i].startsWith('>'); i++) quote.push(lines[i].replace(/^>\s?/, ''))
      i--
      out.push({ kind: 'quote', text: quote.join('\n') })
    } else if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
      const ordered = /^\s*\d+\./.test(line)
      const items: string[] = []
      for (; i < lines.length && lines[i].trim(); i++) {
        const item = lines[i].match(/^\s*(?:[-*]|\d+\.)\s+(.*)$/)
        if (item) items.push(item[1])
        else if (items.length) items[items.length - 1] += ' ' + lines[i].trim()
      }
      i--
      out.push({ kind: 'list', ordered, items })
    } else {
      const para: string[] = []
      for (; i < lines.length && lines[i].trim() && !/^(#{1,6}\s|>|```|\s*([-*]|\d+\.)\s)/.test(lines[i]); i++)
        para.push(lines[i].replace(/ {2}$/, ''))
      i--
      out.push({ kind: 'para', text: para.join('\n') })
    }
  }
  return out
}

// MdDoc renders a ticket field's markdown as Jira shows it, building
// elements rather than HTML so nothing in the ticket can inject markup. The
// line holding the quote, if any, is marked and scrolled to.
export function MdDoc({ text, quote }: { text: string; quote?: string }) {
  return (
    <div className="md-doc">
      {blocks(text).map((b, i) => {
        switch (b.kind) {
          case 'heading': {
            const H = `h${Math.min(6, b.level + 2)}` as 'h3'
            return (
              <H key={i} className="md-h">
                <Text text={b.text} quote={quote} />
              </H>
            )
          }
          case 'rule':
            return <hr key={i} />
          case 'list': {
            const L = b.ordered ? 'ol' : 'ul'
            return (
              <L key={i}>
                {b.items.map((item, j) => (
                  <li key={j}>
                    <Text text={item} quote={quote} />
                  </li>
                ))}
              </L>
            )
          }
          case 'quote':
            return (
              <blockquote key={i} className="prewrap">
                <Text text={b.text} quote={quote} />
              </blockquote>
            )
          case 'code':
            return (
              <pre key={i}>
                <code>{b.text}</code>
              </pre>
            )
          default:
            return (
              <p key={i} className="prewrap">
                <Text text={b.text} quote={quote} />
              </p>
            )
        }
      })}
    </div>
  )
}
