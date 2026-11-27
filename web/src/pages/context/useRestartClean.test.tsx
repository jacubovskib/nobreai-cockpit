import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useRestartClean, type RestartCleanOptions } from './useRestartClean'

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const r = routes[`${init?.method ?? 'GET'} ${url}`]
    if (r === undefined) return new Response('{}', { status: 404 })
    if (r && typeof r === 'object' && 'status' in r && 'body' in r) {
      const o = r as { status: number; body: unknown }
      return new Response(JSON.stringify(o.body), { status: o.status })
    }
    return new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const URL = '/api/sessions/s1/restart-clean'
const bodies = (fn: ReturnType<typeof mock>) =>
  fn.mock.calls.filter(([u, i]) => u === URL && i?.method === 'POST').map(([, i]) => JSON.parse(i?.body ?? '{}'))
function setup(o: Partial<RestartCleanOptions> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  return renderHook(() => useRestartClean({ sessionId: 's1', whereStopped: 'Parei', whereStoppedAt: new Date().toISOString(), ...o }), { wrapper })
}
afterEach(() => vi.unstubAllGlobals())

describe('useRestartClean', () => {
  it('registro recente sem edição: corpo {} ou só o mode', async () => {
    const fn = mock({ [`POST ${URL}`]: { ok: true } })
    const a = setup()
    act(() => a.result.current.send())
    await waitFor(() => expect(a.result.current.done).toBe(true))
    const b = setup({ mode: 'background' })
    act(() => b.result.current.send())
    await waitFor(() => expect(b.result.current.done).toBe(true))
    expect(bodies(fn)).toEqual([{}, { mode: 'background' }])
  })
  it('texto editado com mode vai junto', async () => {
    const fn = mock({ [`POST ${URL}`]: { ok: true } })
    const h = setup({ mode: 'background' })
    act(() => h.result.current.setText('novo'))
    act(() => h.result.current.send())
    await waitFor(() => expect(h.result.current.done).toBe(true))
    expect(bodies(fn)).toEqual([{ where_stopped: 'novo', mode: 'background' }])
  })
  it('sem data de registro exige texto', () => {
    mock({})
    const h = setup({ whereStopped: '', whereStoppedAt: null })
    expect(h.result.current.needsText).toBe(true)
    expect(h.result.current.canSend).toBe(false)
    act(() => h.result.current.setText('x'))
    expect(h.result.current.canSend).toBe(true)
  })
  it('sucesso expõe terminalId e chama onDone uma vez', async () => {
    mock({ [`POST ${URL}`]: { ok: true, terminal_id: 't9' } })
    const onDone = vi.fn()
    const h = setup({ onDone })
    act(() => h.result.current.send())
    await waitFor(() => expect(h.result.current.done).toBe(true))
    expect(h.result.current.terminalId).toBe('t9')
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(onDone).toHaveBeenCalledWith('t9')
  })
  it('erro pedindo o texto passa a exigi-lo', async () => {
    mock({ [`POST ${URL}`]: { status: 400, body: { error: 'registre onde parou antes de recomeçar' } } })
    const h = setup()
    expect(h.result.current.needsText).toBe(false)
    act(() => h.result.current.send())
    await waitFor(() => expect(h.result.current.error?.message).toBe('registre onde parou antes de recomeçar'))
    expect(h.result.current.needsText).toBe(true)
  })
  it('erro 409 fica literal e não conclui', async () => {
    mock({ [`POST ${URL}`]: { status: 409, body: { error: 'sessão sem card' } } })
    const h = setup()
    act(() => h.result.current.send())
    await waitFor(() => expect(h.result.current.error?.message).toBe('sessão sem card'))
    expect(h.result.current.done).toBe(false)
    expect(h.result.current.needsText).toBe(false)
  })
})
