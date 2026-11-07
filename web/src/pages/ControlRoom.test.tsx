import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ControlRoom } from './ControlRoom'
import { CardsList } from './CardsList'
import { Board } from './Board'
import type { Overview, CardSummary } from '../api/types'

const total = { key: '', agent: '', day: '', sessions: 0, input: 0, output: 0, cache_write: 0, cache_read: 0, cost_usd: 0, unpriced: false }
const overview: Overview = {
  active: [],
  today: {
    active_agents: 0, cards_in_progress: 1, cards_without_live_session: 1, cost_usd: 1.5, cost_unpriced: false,
    daily_limit_usd: 33, tokens_input: 1000, tokens_output: 500, tokens_cache_write: 0, tokens_cache_read: 0, per_agent: [],
  },
  alerts: [{ kind: 'stale', card_key: 'NAC-38', agent: 'claude', text: 'NAC-38 está em “Em andamento” sem sessão viva há 12 min.' }],
  no_card: [],
  events: [],
}
const card = (key: string, title: string): CardSummary =>
  ({ key, title, column: 'backlog', agent: 'claude', live: false, session_count: 0, totals: total, readiness: { items: [], met: 0, total: 5, ready: false } }) as unknown as CardSummary

// routes: "GET /api/x" ou "/api/x" (= GET) -> corpo, ou {status, body}, ou função.
type Reply = unknown
function mock(routes: Record<string, Reply>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{}', { status: 404 })
    const x = typeof r === 'function' ? (r as () => unknown)() : r
    if (x && typeof x === 'object' && 'status' in x && 'body' in x) {
      const o = x as { status: number; body: unknown }
      return new Response(JSON.stringify(o.body), { status: o.status })
    }
    return new Response(JSON.stringify(x), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const calls = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

describe('ControlRoom', () => {
  it('mostra o alerta stale e a ação Mover para Pausado', async () => {
    const fn = mock({ '/api/overview': overview, '/api/settings': { currency: 'USD', brl_rate: 0 }, 'PATCH /api/cards/NAC-38': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText(/NAC-38 está em “Em andamento” sem sessão viva há 12 min/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mover para Pausado' })).toBeInTheDocument()
    expect(screen.getByText('1 sem sessão viva')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Mover para Pausado' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/cards/NAC-38')).toEqual([{ column: 'pausado' }]))
  })

  const noCard = {
    id: 's1', agent: 'claude', project_id: 7, cwd: '/home/x/site', started_at: new Date().toISOString(),
    usage: total, unpriced: false,
  }
  it('criar card desta sessão: attach falha (409), retry só refaz o attach', async () => {
    let attachN = 0
    const fn = mock({
      '/api/overview': { ...overview, alerts: [], no_card: [noCard] },
      '/api/settings': {},
      'POST /api/cards': { key: 'NAC-9' },
      'POST /api/cards/NAC-9/attach': () => (++attachN === 1 ? { status: 409, body: { error: 'NAC-9 já tem uma sessão viva.' } } : {}),
    })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Criar card desta sessão' }))
    fireEvent.change(screen.getByLabelText('Título do card'), { target: { value: 'Meu card' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar e vincular' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('NAC-9')
    expect(alert).toHaveTextContent('já tem uma sessão viva')
    expect(calls(fn, 'POST', '/api/cards')).toEqual([{ title: 'Meu card', project_id: 7 }])
    fireEvent.click(screen.getByRole('button', { name: 'Vincular ao card criado' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/cards/NAC-9/attach')).toHaveLength(2))
    expect(calls(fn, 'POST', '/api/cards')).toHaveLength(1)
    expect(calls(fn, 'POST', '/api/cards/NAC-9/attach')[0]).toEqual({ session_id: 's1' })
  })

  it('Nova tarefa cria o card com o projeto escolhido', async () => {
    const fn = mock({
      '/api/overview': { ...overview, alerts: [] }, '/api/settings': {},
      '/api/projects': [{ id: 3, name: 'portal-api', path: '/p', capture: true }],
      'POST /api/cards': { key: 'NAC-10' },
    })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Nova tarefa' }))
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Webhook' } })
    await screen.findByRole('option', { name: 'portal-api' })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/cards')).toEqual([{ title: 'Webhook', goal: '', project_id: 3 }]))
  })

  it('Nova tarefa sem projeto orienta a cadastrar', async () => {
    mock({ '/api/overview': { ...overview, alerts: [] }, '/api/settings': {}, '/api/projects': [] })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Nova tarefa' }))
    expect(await screen.findByRole('link', { name: 'Cadastre um projeto em Configuração' })).toHaveAttribute('href', '/config')
  })
})

describe('Board', () => {
  const mk = (key: string, column: string, agent: string, extra = {}) => ({ ...card(key, 't-' + key), column, agent, ...extra })
  const cards = [mk('NAC-1', 'backlog', 'claude'), mk('NAC-2', 'backlog', 'codex')]

  it('select "Mover para" envia PATCH column', async () => {
    const fn = mock({ '/api/cards': cards, 'PATCH /api/cards/NAC-1': {} })
    wrap(<Board />)
    fireEvent.change(await screen.findByLabelText('Mover NAC-1 para'), { target: { value: 'revisao' } })
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/cards/NAC-1')).toEqual([{ column: 'revisao' }]))
  })

  it('falha no PATCH mostra o erro do servidor e mantém a coluna', async () => {
    mock({ '/api/cards': cards, 'PATCH /api/cards/NAC-1': { status: 409, body: { error: 'movimento recusado' } } })
    wrap(<Board />)
    fireEvent.change(await screen.findByLabelText('Mover NAC-1 para'), { target: { value: 'concluido' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('movimento recusado')
    const backlog = screen.getByRole('region', { name: 'Backlog' })
    expect(within(backlog).getByText('t-NAC-1')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Concluído' })).queryByText('t-NAC-1')).not.toBeInTheDocument()
  })

  it('filtro por agente esconde cards de outros agentes', async () => {
    mock({ '/api/cards': cards })
    wrap(<Board />)
    await screen.findByText('t-NAC-1')
    fireEvent.click(screen.getByRole('button', { name: 'Codex' }))
    expect(screen.queryByText('t-NAC-1')).not.toBeInTheDocument()
    expect(screen.getByText('t-NAC-2')).toBeInTheDocument()
  })

  it('sem last_activity_at mostra "sem sessão viva"', async () => {
    mock({ '/api/cards': [mk('NAC-3', 'em_andamento', 'claude', { last_activity_at: null })] })
    wrap(<Board />)
    expect(await screen.findByText('sem sessão viva')).toBeInTheDocument()
    expect(screen.queryByText(/há 0 min/)).not.toBeInTheDocument()
  })
})

describe('CardsList', () => {
  it('filtra por texto', async () => {
    mock({ '/api/cards': [card('NAC-1', 'Migrar OAuth'), card('NAC-2', 'Cache Redis')] })
    wrap(<CardsList />)
    expect(await screen.findByText('Migrar OAuth')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Buscar cards'), { target: { value: 'redis' } })
    expect(screen.queryByText('Migrar OAuth')).not.toBeInTheDocument()
    expect(screen.getByText('Cache Redis')).toBeInTheDocument()
  })
})
