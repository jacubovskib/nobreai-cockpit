import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CardPage } from './CardPage'
import { Config } from './Config'
import { NewTask } from './NewTask'
import { ContinuePanel } from './card/ContinuePanel'
import { ContextInfo, ContextMeter } from './context/ContextMeter'
import { Credits } from '../ui/Money'
import type { CardDetail, ContextLive } from '../api/types'

vi.mock('../ui/TerminalView', () => ({ TerminalView: () => <div /> }))

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{}', { status: 404 })
    return new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const bodies = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(ui: React.ReactNode, path = '/cards/NAC-42') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}><Routes><Route path="/cards/:key" element={ui} /></Routes></MemoryRouter>
    </QueryClientProvider>,
  )
}
afterEach(() => vi.unstubAllGlobals())

const det = (found: boolean, installable = true, status_line = false) => ({ found, path: '/usr/bin/x', version: '3.1', checks: [{ key: 'hooks', label: 'Hooks', detail: '', ok: false }] , installable, status_line })
const agentsApi = (rows: [string, boolean][]) => rows.map(([agent, found]) => ({ agent, detection: { found, path: '', version: '', checks: [] }, installable: true, status_line: false }))

const tot = (o: object = {}) => ({ key: '', agent: '', day: '', sessions: 1, input: 1000, output: 100, cache_write: 0, cache_read: 0, cost_usd: 4.82, credits: 0, unpriced: false, ...o })
const mkSess = (id: string, agent: string, last: string, usage: object = {}) => ({ id, agent, started_at: '2026-11-05T10:00:00Z', last_event_at: last, ended_at: '2026-11-05T11:00:00Z', usage: tot(usage), unpriced: false, card_id: 1 })
const detail = (sessions: unknown[], totals: object) => ({
  id: 1, key: 'NAC-42', title: 'T', goal: '', done_when: '', how_to_test: '', where_stopped: '', where_stopped_at: null, branch: '', project_id: 1, column: 'revisao',
  content: { decisions: [], files: [], todos: [], questions: [] }, sessions, totals, live: null,
  readiness: { met: 0, total: 0, ready: false, items: [] }, touched_files: [],
}) as unknown as CardDetail
const base = { '/api/settings': { currency: 'USD', brl_rate: 0 }, '/api/limits': [], '/api/projects': [{ id: 1, name: 'portal' }], '/api/prompts': [] }

describe('créditos', () => {
  it('Credits só aparece com créditos > 0, em pt-BR com 2 casas', () => {
    const { container, rerender } = render(<Credits credits={0} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<Credits credits={1234.5} />)
    expect(container).toHaveTextContent('1.234,50 créditos')
  })
  it('card com US$ e créditos mostra os dois, sem somar', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail([mkSess('s1', 'claude', '2026-11-05T10:40:00Z'), mkSess('s2', 'kiro', '2026-11-05T10:50:00Z', { cost_usd: 0, credits: 12.5 })], tot({ cost_usd: 4.82, credits: 12.5 })) })
    wrap(<CardPage />)
    const total = (await screen.findByText(/no total/)).closest('span')!
    expect(total).toHaveTextContent('US$ 4,82 · 12,50 créditos no total')
    expect(screen.getAllByText(/12,50 créditos/).length).toBeGreaterThan(1)
  })
  it('card sem créditos não menciona créditos', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail([mkSess('s1', 'claude', '2026-11-05T10:40:00Z')], tot()) })
    wrap(<CardPage />)
    await screen.findByText(/no total/)
    expect(screen.queryByText(/créditos/)).not.toBeInTheDocument()
  })
})

describe('medidor pct_only', () => {
  it('mostra só a %, sem tokens', () => {
    render(<ContextMeter pctOnly tokens={0} windowTokens={0} pct={42} />)
    expect(screen.getByText('42%')).toBeInTheDocument()
    expect(screen.getByText('janela informada pelo agente')).toBeInTheDocument()
    expect(screen.queryByText(/mil|milhão|\/ 0/)).not.toBeInTheDocument()
  })
  it('detalhe pct_only omite cache, parada e tokens', () => {
    const c = { tokens: 0, model: '', window_tokens: 0, window_estimated: false, pct: 42, pct_only: true, growth_per_turn: 0, turns_to_limit: 0, at_limit: false, turns_since_compact: 3, compactions: 1, cache_hit_pct: 0, recent_rewrite: null, ttl: '', idle_sec: 0, idle_over_ttl: false, top_outputs: [] } as ContextLive
    render(<ContextInfo c={c} />)
    expect(screen.getByText('Compactações')).toBeInTheDocument()
    expect(screen.queryByText('Cache aproveitado')).not.toBeInTheDocument()
    expect(screen.queryByText('Parada')).not.toBeInTheDocument()
  })
})

describe('Configuração por capacidade', () => {
  it('instalável e status line vêm da API, sem nome de agente no código', async () => {
    mock({
      '/api/settings': { rules: {}, currency: 'USD', brl_rate: 0 }, '/api/limits': [], '/api/projects': [], '/api/cards': [], '/api/prices': [], '/api/usage/totals?group=model': [],
      '/api/agents': [
        { agent: 'claude', detection: det(true), installable: true, status_line: true },
        { agent: 'kiro', detection: det(true), installable: true, status_line: false },
        { agent: 'codex', detection: det(false), installable: false, status_line: false },
      ],
    })
    wrap(<Config />)
    const kiro = await screen.findByRole('article', { name: 'Kiro' })
    expect(within(kiro).getByRole('button', { name: 'Instalar o que falta' })).toBeInTheDocument()
    expect(within(kiro).queryByRole('button', { name: /status line|Integrar/ })).not.toBeInTheDocument()
    expect(within(screen.getByRole('article', { name: 'Claude Code' })).getByRole('button', { name: 'Integrar status line' })).toBeInTheDocument()
    const codex = screen.getByRole('article', { name: 'Codex' })
    expect(within(codex).getByText('em breve')).toBeInTheDocument()
    expect(within(codex).queryByRole('button', { name: 'Instalar o que falta' })).not.toBeInTheDocument()
  })
})

describe('seletor do Continuar', () => {
  const routes = { ...base, '/api/agents': agentsApi([['claude', true], ['kiro', true], ['codex', false]]) }
  it('padrão é o agente da última sessão', async () => {
    const fn = mock({ ...routes, 'POST /api/cards/NAC-42/continue': { ok: true } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled lastAgent="kiro" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Iniciar Kiro neste card' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[0]).toMatchObject({ agent: 'kiro' }))
    expect(screen.getByRole('radio', { name: /Codex/ })).toBeDisabled()
  })
  it('sem última sessão (ou ela não instalada), cai no primeiro instalado', async () => {
    mock(routes)
    const { unmount } = wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    expect(await screen.findByRole('button', { name: 'Iniciar Claude Code neste card' })).toBeInTheDocument()
    unmount()
    wrap(<ContinuePanel cardKey="NAC-42" enabled lastAgent="codex" />)
    expect(await screen.findByRole('button', { name: 'Iniciar Claude Code neste card' })).toBeInTheDocument()
  })
  it('a página do card passa o agente da sessão mais recente', async () => {
    mock({ ...routes, '/api/cards/NAC-42': detail([mkSess('s1', 'claude', '2026-11-05T10:00:00Z'), mkSess('s2', 'kiro', '2026-11-06T10:00:00Z')], tot()) })
    wrap(<CardPage />)
    expect(await screen.findByRole('button', { name: 'Iniciar Kiro neste card' })).toBeInTheDocument()
  })
})

describe('seletor da nova tarefa', () => {
  const open = async () => fireEvent.click(await screen.findByRole('button', { name: 'Nova tarefa' }))
  it('com dois instalados mostra o seletor, padrão o primeiro, e envia o escolhido', async () => {
    const fn = mock({ ...base, '/api/agents': agentsApi([['claude', true], ['kiro', true], ['codex', false]]), 'POST /api/cards': { key: 'NAC-1' } })
    wrap(<NewTask />)
    await open()
    const sel = await screen.findByLabelText('Agente')
    expect(sel).toHaveValue('claude')
    expect(Array.from((sel as HTMLSelectElement).options).map((o) => o.text)).toEqual(['Claude Code', 'Kiro'])
    fireEvent.change(sel, { target: { value: 'kiro' } })
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Algo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards')[0]).toMatchObject({ title: 'Algo', agent: 'kiro' }))
  })
  it('com um só instalado esconde o seletor', async () => {
    mock({ ...base, '/api/agents': agentsApi([['claude', false], ['kiro', true]]) })
    wrap(<NewTask />)
    await open()
    await screen.findByLabelText('Título')
    await waitFor(() => expect(screen.queryByLabelText('Agente')).not.toBeInTheDocument())
  })
})
