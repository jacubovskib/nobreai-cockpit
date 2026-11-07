import { Suspense, lazy, type ComponentProps } from 'react'
import type { TerminalView as TerminalViewType } from './TerminalView'

// O xterm (JS + CSS) só é baixado quando algum terminal é aberto.
const Inner = lazy(() => import('./TerminalView').then((m) => ({ default: m.TerminalView })))

export function LazyTerminalView(props: ComponentProps<typeof TerminalViewType>) {
  return (
    <Suspense fallback={<p role="status" style={{ margin: 0, color: 'var(--text-2)' }}>Carregando terminal…</p>}>
      <Inner {...props} />
    </Suspense>
  )
}
