import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Analytics } from './Analytics'
import { Config } from './Config'
import { Prompts } from './Prompts'

type Reply = unknown
function mock(routes: Record<string, Reply>) {
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
const calls = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const settings = {
  rules: { goal: true, where_stopped: true, essential_file: true, todo_open: true, how_to_test: true, no_open_questions: false },
  where_stopped_ttl_min: 15, grace_min: 15, reminder_edit_threshold: 3, launch_mode: 'terminal', terminal_cmd: [],
  currency: 'USD', brl_rate: 0, incomplete_action: 'ask_agent', demo: false,
}
const tot = (o: object) => ({ key: '', agent: '', day: '', sessions: 1, input: 0, output: 0, cache_write: 0, cache_read: 0, cost_usd: 0, unpriced: false, ...o })

const stub = { found: false, path: '', version: '' }
const agents = [
  { agent: 'claude', detection: { found: true, path: '/usr/bin/claude', version: '2.1.0', checks: [
    { key: 'cli', label: 'CLI encontrado', detail: '/usr/bin/claude', ok: true },
    { key: 'hooks', label: 'Hooks instalados', detail: 'faltam: Stop', ok: false },
    { key: 'skill', label: 'Skill do Cockpit instalada', detail: '', ok: true },
    { key: 'events', label: 'Recebendo eventos', detail: 'nenhum evento recebido ainda', ok: false },
  ] } },
  ...['codex', 'kiro'].map((agent) => ({ agent, detection: { ...stub, checks: [{ key: 'adapter', label: 'Adaptador', detail: 'Adaptador chega no plano 2', ok: false }] } })),
]

describe('Config', () => {
  const routes = {
    '/api/settings': settings, '/api/limits': [], '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }],
    '/api/cards': [], '/api/prices': [], '/api/usage/totals?group=model': [tot({ key: 'gpt-x', unpriced: true })],
    'PUT /api/settings': settings, 'PUT /api/limits': {}, 'PATCH /api/projects/1': {}, 'POST /api/projects': {},
  }

  it('salva a regra de handoff com o mapa completo de regras', async () => {
    const fn = mock(routes)
    wrap(<Config />)
    const box = await screen.findByLabelText('“Como testar” preenchido')
    fireEvent.click(box)
    const section = screen.getByRole('region', { name: /pronto para handoff/ })
    fireEvent.click(within(section).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    const body = calls(fn, 'PUT', '/api/settings')[0]
    expect(body.rules).toEqual({ ...settings.rules, how_to_test: false })
    expect(body.where_stopped_ttl_min).toBe(15)
  })

  it('limites enviam só pause|warn por agente', async () => {
    const fn = mock(routes)
    wrap(<Config />)
    fireEvent.change(await screen.findByLabelText('Limite diário de Codex'), { target: { value: '10,5' } })
    fireEvent.change(screen.getByLabelText('Ação em 100% para Codex'), { target: { value: 'pause' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar limites' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/limits')).toHaveLength(3))
    expect(calls(fn, 'PUT', '/api/limits')[1]).toEqual({ agent: 'codex', daily_usd: 10.5, card_usd: 0, warn_pct: 80, action: 'pause' })
  })

  it('destaca modelo visto no uso sem preço', async () => {
    mock({ ...routes, '/api/agents': agents })
    wrap(<Config />)
    expect(await screen.findByText(/visto no uso, sem preço/)).toBeInTheDocument()
  })

  it('lista agentes detectados; codex e kiro ficam em breve', async () => {
    mock({ ...routes, '/api/agents': agents })
    wrap(<Config />)
    const claude = await screen.findByRole('article', { name: 'Claude Code' })
    expect(within(claude).getByText('detectado')).toBeInTheDocument()
    expect(within(claude).getByText('Hooks instalados')).toBeInTheDocument()
    expect(within(claude).getByText('faltam: Stop')).toBeInTheDocument()
    const sec = screen.getByRole('region', { name: 'Agentes detectados' })
    expect(within(sec).getAllByText('em breve')).toHaveLength(2)
    expect(screen.getAllByText('Adaptador chega no plano 2')).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: 'Instalar o que falta' })).toHaveLength(1)
  })

  it('instalar o que falta chama o POST e mostra mensagens e backup', async () => {
    const fn = mock({ ...routes, '/api/agents': agents,
      'POST /api/agents/claude/install': { changed: ['Stop'], backup: '/h/.claude/settings.json.bak-20261107130405', messages: ['Hooks instalados em Stop.'] } })
    wrap(<Config />)
    fireEvent.click(await screen.findByRole('button', { name: 'Instalar o que falta' }))
    expect(await screen.findByText('Hooks instalados em Stop.')).toBeInTheDocument()
    expect(screen.getByText('/h/.claude/settings.json.bak-20261107130405')).toBeInTheDocument()
    expect(fn.mock.calls.some(([u, i]) => u === '/api/agents/claude/install' && i?.method === 'POST')).toBe(true)
  })

  it('erro na instalação aparece na tela', async () => {
    mock({ ...routes, '/api/agents': agents,
      'POST /api/agents/claude/install': { status: 500, body: { error: '~/.claude/settings.json não é um JSON válido; nada foi alterado' } } })
    wrap(<Config />)
    fireEvent.click(await screen.findByRole('button', { name: 'Instalar o que falta' }))
    expect(await screen.findByText(/não é um JSON válido/)).toBeInTheDocument()
  })

  it('preço vazio mostra erro e não envia; preço válido envia o PUT', async () => {
    const fn = mock({ ...routes, 'PUT /api/prices': {} })
    wrap(<Config />)
    fireEvent.click(await screen.findByRole('button', { name: 'Salvar preço de gpt-x' }))
    expect(await screen.findByText('Informe os preços de entrada e saída (maiores que zero)')).toBeInTheDocument()
    expect(calls(fn, 'PUT', '/api/prices')).toHaveLength(0)
    fireEvent.change(screen.getByLabelText('Preço de entrada de gpt-x'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Preço de saída de gpt-x'), { target: { value: '15,5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar preço de gpt-x' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/prices')).toEqual([{ model: 'gpt-x', input_per_m: 3, output_per_m: 15.5, cache_write_per_m: null, cache_read_per_m: null }]))
  })

  it('cache em branco = desconhecido (null); 0 explícito = grátis', async () => {
    const fn = mock({ ...routes, 'PUT /api/prices': {} })
    wrap(<Config />)
    expect(await screen.findByText(/em branco = desconhecido/)).toBeInTheDocument()
    fireEvent.change(await screen.findByLabelText('Preço de entrada de gpt-x'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Preço de saída de gpt-x'), { target: { value: '15' } })
    fireEvent.change(screen.getByLabelText('Preço de cache (leitura) de gpt-x'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar preço de gpt-x' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/prices')).toEqual([{ model: 'gpt-x', input_per_m: 3, output_per_m: 15, cache_write_per_m: null, cache_read_per_m: 0 }]))
  })

  it('preço salvo com cache nulo mostra o campo vazio', async () => {
    mock({ ...routes, '/api/prices': [{ model: 'gpt-x', input_per_m: 3, output_per_m: 15, cache_write_per_m: null, cache_read_per_m: 0.3 }] })
    wrap(<Config />)
    expect(await screen.findByLabelText('Preço de cache (escrita) de gpt-x')).toHaveValue('')
    expect(screen.getByLabelText('Preço de cache (leitura) de gpt-x')).toHaveValue('0,3')
  })

  it('adiciona projeto com caminho', async () => {
    const fn = mock(routes)
    wrap(<Config />)
    fireEvent.click(await screen.findByRole('button', { name: 'Adicionar repositório' }))
    fireEvent.change(screen.getByLabelText('Caminho absoluto'), { target: { value: '/w/novo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Adicionar' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/projects')).toEqual([{ name: '', path: '/w/novo' }]))
  })
})

describe('Prompts', () => {
  const prompt = { id: 1, name: 'Retomar', slug: 'retomar', tag: 'Retomada', description: 'd', body: 'Oi {{card.id}} {{xx}}', agents: ['claude'], project_id: null, is_default_handoff: false, uses: 2, last_used_at: null }
  const routes = {
    '/api/prompts': [prompt], '/api/projects': [], '/api/cards': [{ key: 'NAC-1', title: 'T', id: 1 }],
    '/api/events?kind=prompt&limit=50': [{ id: 9, session_id: 's', card_id: 1, kind: 'prompt', data: { text: 'faz isso' }, at: new Date().toISOString(), agent: 'codex', card_key: 'NAC-1' }],
    'POST /api/prompts/render': { status: 422, body: { error: 'variáveis desconhecidas: xx', unknown: ['xx'] } },
    'POST /api/prompts': { ...prompt, slug: 'faz-isso' },
  }

  it('prévia com variável desconhecida mostra o erro 422', async () => {
    const fn = mock(routes)
    wrap(<Prompts />)
    fireEvent.click(await screen.findByRole('button', { name: /Retomar/ }))
    expect(await screen.findByText(/Variável desconhecida: \{\{xx\}\}/)).toBeInTheDocument()
    expect(calls(fn, 'POST', '/api/prompts/render')[0]).toEqual({ card: 'NAC-1', body: 'Oi {{card.id}} {{xx}}' })
    expect(screen.getByText(/nobre prompt use retomar --card NAC-1/)).toBeInTheDocument()
  })

  it('"Salvar como prompt" abre o editor com o texto capturado e cria', async () => {
    const fn = mock(routes)
    wrap(<Prompts />)
    fireEvent.click(await screen.findByRole('button', { name: /Salvar como prompt/ }))
    expect(screen.getByLabelText('Texto')).toHaveValue('faz isso')
    expect(screen.getByLabelText('Atalho no CLI')).toHaveValue('faz-isso')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/prompts')).toHaveLength(1))
    expect(calls(fn, 'POST', '/api/prompts')[0]).toMatchObject({ slug: 'faz-isso', body: 'faz isso', name: 'faz isso' })
  })
})

describe('Analytics', () => {
  it('mostra "sem preço" e "sem card", nunca zero, e alterna para a tabela', async () => {
    const today = new Date()
    const day = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
    const q = (g: string) => new RegExp(`/api/usage/totals\\?group=${g}&`)
    const fn = vi.fn(async (url: string) => {
      const ok = (b: unknown) => new Response(JSON.stringify(b), { status: 200 })
      if (q('day_agent').test(url)) return ok([tot({ day, agent: 'codex', unpriced: true, key: 'x' })])
      if (q('agent').test(url)) return ok([tot({ key: 'codex', agent: 'codex', unpriced: true })])
      if (q('card').test(url)) return ok([tot({ key: '', cost_usd: 2 })])
      if (url.startsWith('/api/sessions')) return ok([])
      if (url === '/api/settings') return ok(settings)
      if (url === '/api/cards') return ok([])
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fn)
    wrap(<Analytics />)
    expect(await screen.findByRole('img', { name: /Codex sem preço/ })).toBeInTheDocument()
    expect(await screen.findByText('sem card')).toBeInTheDocument()
    // Por agente: entrada, saída e cache, não só o total.
    const ag = screen.getByRole('region', { name: /Por agente/ })
    for (const h of ['Entrada', 'Saída', 'Cache']) expect(within(ag).getByRole('columnheader', { name: h })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ver como tabela' }))
    expect(screen.getByRole('columnheader', { name: 'Codex' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '30 dias' }))
    await waitFor(() => expect(fn.mock.calls.some(([u]) => String(u).includes('group=day_agent') && String(u).includes('from='))).toBe(true))
  })
})
