import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ContextMeter, ContextInfo } from './ContextMeter'
import { RestartCleanDialog } from './RestartCleanDialog'
import { ControlRoomContext } from './ContextSessions'
import type { ContextLive } from '../../api/types'

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
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
const bodies = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const ctx = (o: Partial<ContextLive> = {}): ContextLive => ({
  tokens: 620000, model: 'claude-opus-5-5', window_tokens: 1000000, window_estimated: false, pct: 62, growth_per_turn: 12000,
  turns_to_limit: 25, at_limit: false, turns_since_compact: 40, compactions: 2, cache_hit_pct: 91.4, recent_rewrite: null,
  ttl: '5m', idle_sec: 120, idle_over_ttl: false,
  top_outputs: [{ tool: 'Bash', key: 'go test ./...', tokens: 30000, at: '2026-11-20T10:00:00Z' }, { tool: 'Read', key: 'Read big.log', tokens: 20000, at: '2026-11-20T10:01:00Z' }],
  ...o,
})

describe('ContextMeter', () => {
  it('mostra tokens, percentual e janela', () => {
    render(<ContextMeter tokens={620000} windowTokens={1000000} pct={62} />)
    expect(screen.getByText('62% · 620 mil / 1 milhão')).toBeInTheDocument()
    expect(screen.queryByText('janela estimada')).not.toBeInTheDocument()
    expect(screen.getByTestId('context-meter')).toHaveAttribute('data-over', 'false')
  })
  it('marca janela estimada e trava a barra em 100%', () => {
    render(<ContextMeter tokens={250000} windowTokens={200000} pct={125} estimated />)
    expect(screen.getByText('janela estimada')).toBeInTheDocument()
    expect(screen.getByTestId('context-bar')).toHaveStyle({ width: '100%' })
    expect(screen.getByTestId('context-meter')).toHaveAttribute('data-over', 'true')
  })
  it('destaca acima de 80%, regravação recente e parada além do cache', () => {
    render(<ContextMeter tokens={170000} windowTokens={200000} pct={85} rewrite idleOverTTL />)
    expect(screen.getByTestId('context-meter')).toHaveAttribute('data-over', 'true')
    expect(screen.getByText('regravação recente')).toBeInTheDocument()
    expect(screen.getByText('parada além do cache')).toBeInTheDocument()
  })
  it('80% exatos não destaca', () => {
    render(<ContextMeter tokens={160000} windowTokens={200000} pct={80} />)
    expect(screen.getByTestId('context-meter')).toHaveAttribute('data-over', 'false')
  })
})

describe('ContextInfo', () => {
  it('detalha crescimento, turnos até o limite, cache e maiores saídas', () => {
    render(<ContextInfo c={ctx()} />)
    expect(screen.getByText('12 mil tokens')).toBeInTheDocument()
    expect(screen.getByText('25')).toBeInTheDocument()
    expect(screen.getByText('91%')).toBeInTheDocument()
    expect(screen.getByText('$ go test ./...')).toBeInTheDocument()
    expect(screen.getByText('Read big.log')).toBeInTheDocument()
  })
  it('no limite', () => {
    render(<ContextInfo c={ctx({ at_limit: true, turns_to_limit: 0 })} />)
    expect(screen.getByText('no limite')).toBeInTheDocument()
  })
})

describe('ControlRoomContext', () => {
  const live = [{ id: 's1', card_key: 'NAC-1', card_title: 'Tarefa', tokens: 850000, window_tokens: 1000000, pct: 85, over80: true, rewrite: true, idle_over_ttl: false }]
  it('mostra o medidor e abre o detalhe ao clicar', async () => {
    mock({ '/api/context/live': live, '/api/sessions/s1/context': ctx({ pct: 85, tokens: 850000, recent_rewrite: { tokens: 90000, at: '2026-11-20T10:00:00Z' } }) })
    wrap(<ControlRoomContext />)
    expect(await screen.findByText('85% · 850 mil / 1 milhão')).toBeInTheDocument()
    expect(screen.getByText('regravação recente')).toBeInTheDocument()
    expect(screen.queryByText('Cache aproveitado')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ver detalhes' }))
    expect(await screen.findByText('Cache aproveitado')).toBeInTheDocument()
  })
})

describe('RestartCleanDialog', () => {
  const fresh = new Date().toISOString()
  const old = new Date(Date.now() - 2 * 3600_000).toISOString()
  it('com registro recente e sem edição, envia sem texto e mostra o terminal aberto', async () => {
    const fn = mock({ 'POST /api/sessions/s1/restart-clean': { ok: true, terminal_id: 't9' } })
    wrap(<RestartCleanDialog sessionId="s1" whereStopped="Parei no passo 3" whereStoppedAt={fresh} onClose={() => {}} />)
    expect(screen.getByRole('textbox')).toHaveValue('Parei no passo 3')
    fireEvent.click(screen.getByRole('button', { name: 'Recomeçar limpo' }))
    expect(await screen.findByText('Terminal novo aberto.')).toBeInTheDocument()
    expect(bodies(fn, 'POST', '/api/sessions/s1/restart-clean')).toEqual([{}])
  })
  it('texto editado vai no corpo', async () => {
    const fn = mock({ 'POST /api/sessions/s1/restart-clean': { ok: true } })
    wrap(<RestartCleanDialog sessionId="s1" whereStopped="velho" whereStoppedAt={fresh} onClose={() => {}} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'novo texto' } })
    fireEvent.click(screen.getByRole('button', { name: 'Recomeçar limpo' }))
    await screen.findByText('Terminal novo aberto.')
    expect(bodies(fn, 'POST', '/api/sessions/s1/restart-clean')).toEqual([{ where_stopped: 'novo texto' }])
  })
  it('registro velho exige texto e o envia', async () => {
    const fn = mock({ 'POST /api/sessions/s1/restart-clean': { ok: true } })
    wrap(<RestartCleanDialog sessionId="s1" whereStopped="" whereStoppedAt={old} onClose={() => {}} />)
    expect(screen.getByRole('button', { name: 'Recomeçar limpo' })).toBeDisabled()
    expect(screen.getByText(/velho ou vazio/)).toBeInTheDocument()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'onde parei agora' } })
    fireEvent.click(screen.getByRole('button', { name: 'Recomeçar limpo' }))
    await screen.findByText('Terminal novo aberto.')
    expect(bodies(fn, 'POST', '/api/sessions/s1/restart-clean')).toEqual([{ where_stopped: 'onde parei agora' }])
  })
  it('mostra o erro do servidor literalmente e, se pediu o texto, passa a exigi-lo', async () => {
    const fn = mock({ 'POST /api/sessions/s1/restart-clean': { status: 400, body: { error: 'registre onde parou antes de recomeçar' } } })
    wrap(<RestartCleanDialog sessionId="s1" whereStopped="x" whereStoppedAt={fresh} onClose={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: 'Recomeçar limpo' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('registre onde parou antes de recomeçar')
    await waitFor(() => expect(screen.getByText(/velho ou vazio/)).toBeInTheDocument())
    expect(bodies(fn, 'POST', '/api/sessions/s1/restart-clean')).toHaveLength(1)
    const sec = screen.getByRole('region', { name: 'Recomeçar limpo' })
    expect(within(sec).getByRole('button', { name: 'Recomeçar limpo' })).toBeEnabled()
  })
})
