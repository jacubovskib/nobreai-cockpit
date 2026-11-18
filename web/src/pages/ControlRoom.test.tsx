import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ControlRoom } from './ControlRoom'
import { CardsList } from './CardsList'
import { Board } from './Board'
import type { Overview, CardSummary } from '../api/types'
import { CurrencyContext } from '../ui/money'

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
function wrap(ui: React.ReactNode, at = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[at]}>{ui}</MemoryRouter></QueryClientProvider>)
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
    const alert = (await screen.findByText(/já tem uma sessão viva/)).closest('[role="alert"]')!
    expect(alert).toHaveTextContent('NAC-9')
    expect(alert).toHaveTextContent('já tem uma sessão viva')
    expect(calls(fn, 'POST', '/api/cards')).toEqual([{ title: 'Meu card', project_id: 7 }])
    fireEvent.click(screen.getByRole('button', { name: 'Vincular ao card criado' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/cards/NAC-9/attach')).toHaveLength(2))
    expect(calls(fn, 'POST', '/api/cards')).toHaveLength(1)
    expect(calls(fn, 'POST', '/api/cards/NAC-9/attach')[0]).toEqual({ session_id: 's1' })
  })

  it('sessão com processo morto mostra "processo encerrado há N min", não "Ao vivo"', async () => {
    const dead = {
      id: 'claude:1', agent: 'claude', model: 'opus', started_at: new Date(Date.now() - 3600_000).toISOString(),
      pid_dead_at: new Date(Date.now() - 7 * 60_000).toISOString(), ended_at: null,
      card: { key: 'NAC-38', title: 'OAuth' }, last_activity: '', session_usage: total, card_totals: total, readiness: null,
    }
    mock({ '/api/overview': { ...overview, active: [dead] }, '/api/settings': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText('processo encerrado há 7 min')).toBeInTheDocument()
    expect(screen.queryByText(/Ao vivo/)).not.toBeInTheDocument()
  })

  it('Ver terminal casa a sessão pelo pid e, sem pid igual, pelo card', async () => {
    const sess = (id: string, pid: number, key: string | null) => ({
      id, agent: 'claude', model: 'opus', pid, started_at: new Date().toISOString(), pid_dead_at: null, ended_at: null,
      card: key ? { key, title: key } : null, last_activity: '', session_usage: total, card_totals: total, readiness: null,
    })
    const term = (id: string, pid: number, card_key: string) => ({ id, card_key, agent: 'claude', origin: 'nobre run', title: '', pid, started_at: new Date().toISOString(), ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 1 })
    mock({
      '/api/overview': { ...overview, alerts: [], active: [sess('a', 100, null), sess('b', 200, 'NAC-5'), sess('c', 300, 'NAC-6'), sess('d', 400, null)] },
      '/api/settings': {},
      // tD é do card NAC-5 mas pertence à sessão d (pid 400): não serve de fallback para b.
      '/api/terminals': [term('tA', 100, ''), term('tD', 400, 'NAC-5'), term('tB', 999, 'NAC-5'), term('tC', 300, 'NAC-6')],
    })
    wrap(<ControlRoom />)
    await waitFor(() => expect(screen.getAllByRole('link', { name: 'Ver terminal' })).toHaveLength(4))
    const hrefs = screen.getAllByRole('link', { name: 'Ver terminal' }).map((l) => l.getAttribute('href'))
    expect(hrefs).toEqual(['/terminals?id=tA', '/terminals?id=tB', '/terminals?id=tC', '/terminals?id=tD'])
  })

  it('limite: texto segue a ação, marcador em warn_pct e "+ sem preço" no uso misto', async () => {
    const per_agent = [
      { agent: 'claude', spent_usd: 5, daily_usd: 10, pct: 50, warn_pct: 60, action: 'pause', unpriced: true },
      { agent: 'codex', spent_usd: 1, daily_usd: 10, pct: 10, warn_pct: 80, action: 'warn', unpriced: false },
    ]
    mock({ '/api/overview': { ...overview, today: { ...overview.today, per_agent } }, '/api/settings': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText(/Claude Code: em 100% o agente é bloqueado \(a partir da próxima etapa\)/)).toBeInTheDocument()
    expect(screen.getByText(/Codex: em 100% só avisa/)).toBeInTheDocument()
    expect(screen.queryByText(/a sessão é pausada/)).not.toBeInTheDocument()
    expect(screen.getByTestId('warn-marker-claude')).toHaveStyle({ left: '60%' })
    expect(screen.getByTestId('warn-marker-codex')).toHaveStyle({ left: '80%' })
    expect(screen.getByText('+ sem preço')).toBeInTheDocument()
  })

  it('sessão sem card encerrada aparece marcada e permite criar card', async () => {
    const ended = { ...noCard, id: 's2', ended_at: new Date(Date.now() - 3600_000).toISOString() }
    mock({ '/api/overview': { ...overview, alerts: [], no_card: [ended] }, '/api/settings': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText('encerrada')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Criar card desta sessão' })).toBeInTheDocument()
  })

  it('sessão sem card com processo morto aparece como "processo encerrado"', async () => {
    const dead = { ...noCard, id: 's3', ended_at: null, pid_dead_at: new Date(Date.now() - 5 * 60_000).toISOString() }
    mock({ '/api/overview': { ...overview, alerts: [], no_card: [dead] }, '/api/settings': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText('processo encerrado')).toBeInTheDocument()
    expect(screen.queryByText(/\d+ min/)).not.toBeInTheDocument()
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

  it('processo morto mostra "processo encerrado há N min", não "ao vivo"', async () => {
    const pid_dead_at = new Date(Date.now() - 5 * 60_000).toISOString()
    mock({ '/api/cards': [mk('NAC-4', 'em_andamento', 'claude', { live: false, pid_dead_at, last_activity_at: pid_dead_at })] })
    wrap(<Board />)
    expect(await screen.findByText('processo encerrado há 5 min')).toBeInTheDocument()
    expect(screen.queryByText('ao vivo')).not.toBeInTheDocument()
  })

  it('explica que Em andamento exige sessão viva', async () => {
    mock({ '/api/cards': cards })
    wrap(<Board />)
    expect(await screen.findByText(/“Em andamento” só aceita card com sessão viva/)).toBeInTheDocument()
    expect(screen.queryByText(/o daemon reconcilia/)).not.toBeInTheDocument()
  })

  it('custo respeita BRL da configuração', async () => {
    mock({ '/api/cards': [mk('NAC-5', 'backlog', 'claude', { totals: { ...total, cost_usd: 2 } })] })
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}><MemoryRouter>
        <CurrencyContext.Provider value={{ currency: 'BRL', rate: 5 }}><Board /></CurrencyContext.Provider>
      </MemoryRouter></QueryClientProvider>,
    )
    expect(await screen.findByText('R$ 10,00')).toBeInTheDocument()
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

describe('ControlRoom · worktrees', () => {
  it('mostra o worktree de cada agente ativo e liga à página de worktrees', async () => {
    const active = {
      id: 'claude:wt', agent: 'claude', model: 'claude-opus-5-5', started_at: new Date().toISOString(), pid_dead_at: null,
      cwd: '/w/api-oauth/auth', worktree: '/w/api-oauth', worktree_linked: true, branch: 'feat/oauth',
      card: null, last_activity: '', session_usage: total, card_totals: total, readiness: null,
    }
    mock({ '/api/overview': { ...overview, alerts: [], active: [active] }, '/api/settings': {} })
    wrap(<ControlRoom />)
    const tag = await screen.findByRole('link', { name: 'Worktree api-oauth, branch feat/oauth' })
    expect(tag).toHaveAttribute('href', '/worktrees')
    expect(tag).toHaveAttribute('title', '/w/api-oauth')
  })
})

describe('ControlRoom · projeto', () => {
  const base = { agent: 'claude', model: 'm', started_at: new Date().toISOString(), pid_dead_at: null, worktree: '', worktree_linked: false, branch: '', card: null, last_activity: '', session_usage: total, card_totals: total, readiness: null }
  const routes = {
    '/api/overview': { ...overview, alerts: [], active: [{ ...base, id: 'a', project_id: 2 }, { ...base, id: 'b', project_id: null }] },
    '/api/settings': {}, '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'site', path: '/w/site', capture: true }],
  }
  for (const at of ['/', '/?projeto=1']) {
    it(`ignora o seletor (${at}) e mostra as etiquetas`, async () => {
      mock(routes)
      wrap(<ControlRoom />, at)
      expect(await screen.findByText('site')).toBeInTheDocument()
      expect(screen.getByText('Sem projeto')).toBeInTheDocument()
      expect(screen.getAllByText(/^Ao vivo · /)).toHaveLength(2)
    })
  }
  it('sessão sem card mostra a etiqueta do projeto', async () => {
    mock({
      '/api/overview': { ...overview, alerts: [], no_card: [{ id: 's9', agent: 'claude', project_id: 1, cwd: '/w/api', started_at: new Date().toISOString(), usage: total, unpriced: false }] },
      '/api/settings': {}, '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }],
    })
    wrap(<ControlRoom />, '/')
    expect(await screen.findByText('api')).toBeInTheDocument()
  })

  const integ = (key: string, action: string, text: string) => ({
    ...overview, alerts: [{ kind: 'integration', card_key: '', agent: 'claude', text, integration: { key, text, action, again: false } }],
  })

  it('integration alert asks before reinstalling', async () => {
    const fn = mock({
      '/api/overview': integ('hooks', 'reinstall', 'Os hooks do Cockpit sumiram do ~/.claude/settings.json.'),
      '/api/settings': {},
      '/api/agents/claude/install/plan': { changed: ['hooks'], backup: '', messages: [] },
      'POST /api/agents/claude/install': { changed: ['hooks'], backup: '/b', messages: ['Hooks instalados.'] },
    })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Reinstalar' }))
    expect(await screen.findByText('Vai alterar: hooks. O resto do arquivo não muda.')).toBeInTheDocument()
    expect(calls(fn, 'POST', '/api/agents/claude/install')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/agents/claude/install')).toHaveLength(1))
    expect(await screen.findByText('Hooks instalados.')).toBeInTheDocument()
    expect(screen.getByText(/reinicie-as/)).toBeInTheDocument()
  })

  it('statusline alert offers keep current', async () => {
    const fn = mock({
      '/api/overview': integ('statusline', 'statusline', 'A status line do Cockpit foi trocada por outro comando.'),
      '/api/settings': {},
      'POST /api/agents/claude/statusline': {},
      'POST /api/integration/statusline/ignore': {},
    })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Restaurar a do Cockpit' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/agents/claude/statusline')).toEqual([{ action: 'install' }]))
    fireEvent.click(screen.getByRole('button', { name: 'Manter a atual' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/integration/statusline/ignore')).toHaveLength(1))
  })

  it('ignore posts to the item key', async () => {
    const fn = mock({
      '/api/overview': integ('hooks', 'reinstall', 'Os hooks do Cockpit sumiram.'),
      '/api/settings': {},
      'POST /api/integration/hooks/ignore': {},
    })
    wrap(<ControlRoom />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ignorar' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/integration/hooks/ignore')).toHaveLength(1))
  })

  it('restart alert has no buttons', async () => {
    mock({ '/api/overview': integ('daemon', 'restart', 'O Cockpit foi atualizado; reinicie o daemon (make restart).'), '/api/settings': {} })
    wrap(<ControlRoom />)
    expect(await screen.findByText('make restart')).toHaveClass('mono')
    expect(screen.queryByRole('button', { name: 'Reinstalar' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ignorar' })).not.toBeInTheDocument()
  })
})
