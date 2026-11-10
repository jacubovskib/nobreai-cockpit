import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Board } from './Board'

function mock(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    return r === undefined ? new Response('{}', { status: 404 }) : new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap(ui: React.ReactNode, at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[at]}>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const ago = (h: number) => new Date(Date.now() - h * 3600000).toISOString()
const card = (o: object) => ({
  id: 1, key: 'NAC-1', title: 'x', goal: '', done_when: '', where_stopped: '', column: 'backlog', project_id: 1,
  agent: '', session_count: 0, live: false, pid_dead_at: null, last_activity_at: null,
  totals: { cost_usd: 0, unpriced: false }, readiness: { met: 0, total: 5, ready: false },
  signal: '', signal_since: null, ...o,
})
const routes = {
  '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'web', path: '/w/web', capture: true }],
  '/api/cards': [
    card({ id: 1, key: 'NAC-1', title: 'Card parado', column: 'em_andamento', project_id: 1, signal: 'parado', signal_since: ago(50), last_activity_at: ago(50) }),
    card({ id: 2, key: 'NAC-2', title: 'Card revisão', column: 'revisao', project_id: 1, signal: 'aguardando_revisao', signal_since: ago(73) }),
    card({ id: 3, key: 'NAC-3', title: 'Card do web', column: 'backlog', project_id: 2 }),
  ],
}

describe('Board por projeto', () => {
  it('em Todos mostra os 3 cards com a etiqueta do projeto', async () => {
    mock(routes)
    wrap(<Board />, '/board')
    expect(await screen.findByText('Card parado')).toBeInTheDocument()
    expect(screen.getByText('Card revisão')).toBeInTheDocument()
    expect(screen.getByText('Card do web')).toBeInTheDocument()
    expect(screen.getAllByText('api')).toHaveLength(2)
    expect(screen.getAllByText('web')).toHaveLength(1)
  })
  it('mostra o sinal de cada card, sem chamar revisão de parado', async () => {
    mock(routes)
    wrap(<Board />, '/board')
    await screen.findByText('Card parado')
    expect(screen.getByText('parado há 2 dias')).toBeInTheDocument()
    const rev = screen.getByRole('article', { name: /NAC-2/ })
    expect(rev).toHaveTextContent('aguardando revisão há 3 dias')
    expect(rev).not.toHaveTextContent(/parado/)
    expect(screen.getByRole('article', { name: /NAC-1/ })).not.toHaveTextContent('sem sessão')
  })
  it('com ?projeto=2 mostra só o card do projeto, sem etiqueta', async () => {
    mock(routes)
    wrap(<Board />, '/board?projeto=2')
    expect(await screen.findByText('Card do web')).toBeInTheDocument()
    expect(screen.queryByText('Card parado')).toBeNull()
    expect(screen.queryByText('Card revisão')).toBeNull()
    expect(screen.queryByText('web')).toBeNull()
  })
})
