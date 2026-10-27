import { ChevronRight, Folder, KeyRound, Search, SquareKanban, type LucideProps } from 'lucide-react'

const icons = { search: Search, tickets: SquareKanban, folder: Folder, chevron: ChevronRight, account: KeyRound }

// Icon draws a small stroke icon in the text color.
export function Icon({ name, ...props }: { name: keyof typeof icons } & LucideProps) {
  const Svg = icons[name]
  return <Svg size={14} strokeWidth={1.75} aria-hidden="true" {...props} />
}
