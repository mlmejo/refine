import type { ReactNode } from 'react'

// pattern matches the inline markdown Claude writes: `code`, **bold** and
// [text](https://link). An unmatched marker stays as it is.
const pattern = /`([^`\n]+)`|\*\*([^*\n]+)\*\*|\[([^\]\n]+)\]\((https:\/\/[^)\s]+)\)/g

// Md renders text's inline markdown, building elements rather than HTML so
// nothing in the text can inject markup.
export function Md({ text }: { text: string | null | undefined }) {
  if (!text) return null
  const out: ReactNode[] = []
  let last = 0
  for (const match of text.matchAll(pattern)) {
    const at = match.index ?? 0
    const [, code, bold, label, url] = match
    out.push(text.slice(last, at))
    if (code !== undefined) out.push(<code key={at}>{code}</code>)
    else if (bold !== undefined) out.push(<strong key={at}>{bold}</strong>)
    else
      out.push(
        <a key={at} href={url} target="_blank" rel="noreferrer">
          {label}
        </a>
      )
    last = at + match[0].length
  }
  out.push(text.slice(last))
  return <>{out}</>
}
