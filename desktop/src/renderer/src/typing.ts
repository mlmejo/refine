// typing reports whether a key went to a text field, where letters are text
// rather than shortcuts.
export function typing(e: KeyboardEvent): boolean {
  const target = e.target
  return target instanceof Element && !!target.closest('input, textarea, [contenteditable]')
}
