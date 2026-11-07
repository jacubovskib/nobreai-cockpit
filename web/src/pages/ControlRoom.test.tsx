import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ControlRoom } from './ControlRoom'
import { CardsList } from './CardsList'
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

function mock(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (!(url in routes)) return new Response('{}', { status: 404 })
    return new Response(JSON.stringify(routes[url]), { status: 200 })
  }))
}
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

describe('ControlRoom', () => {
  it('mostra o alerta stale e a ação Mover para Pausado', async () => {
    mock({ '/api/overview': overview, '/api/settings': { currency: 'USD', brl_rate: 0 } })
    wrap(<ControlRoom />)
    expect(await screen.findByText(/NAC-38 está em “Em andamento” sem sessão viva há 12 min/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mover para Pausado' })).toBeInTheDocument()
    expect(screen.getByText('1 sem sessão viva')).toBeInTheDocument()
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
