import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useContextLive, useSessionContext } from './queries'

afterEach(() => vi.unstubAllGlobals())

// Sessão parada não dispara hook nem evento: o medidor se atualiza sozinho
// (tempo parado e "parada além do cache").
describe('medidor de contexto sem eventos', () => {
  it.each<[string, () => unknown]>([
    ['context-live', () => useContextLive()],
    ['session-context', () => useSessionContext('claude:x')],
  ])('%s refaz a consulta a cada 30 s', (key, hook) => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { status: 200 })))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    renderHook(hook, { wrapper })
    const q = qc.getQueryCache().findAll({ queryKey: [key] })[0]
    expect((q.options as { refetchInterval?: number }).refetchInterval).toBe(30_000)
  })
})
