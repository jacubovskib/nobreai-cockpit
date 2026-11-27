import { Suspense, forwardRef, lazy } from 'react'
import type { TerminalHandle, TerminalViewProps } from './TerminalView'

// O xterm (JS + CSS) só é baixado quando algum terminal é aberto.
const Inner = lazy(() => import('./TerminalView').then((m) => ({ default: m.TerminalView })))

// O ref chega ao TerminalView carregado: o tile da grade o comanda por ele.
export const LazyTerminalView = forwardRef<TerminalHandle, TerminalViewProps>(function LazyTerminalView(props, ref) {
  return (
    <Suspense fallback={<p role="status" style={{ margin: 0, color: 'var(--text-2)' }}>Carregando terminal…</p>}>
      <Inner {...props} ref={ref} />
    </Suspense>
  )
})
