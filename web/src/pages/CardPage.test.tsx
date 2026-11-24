import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CardPage } from './CardPage'
import { ContinuePanel } from './card/ContinuePanel'
import type { CardDetail } from '../api/types'

vi.mock('../ui/TerminalView', () => ({ TerminalView: ({ id, readonly }: { id: string; readonly?: boolean }) => <div data-testid="tv">{id}{readonly ? ' (somente assistir)' : ''}</div> }))

const tot = (o: object = {}) => ({ key: '', agent: '', day: '', sessions: 1, input: 820000, output: 121000, cache_write: 100000, cache_read: 41000, cost_usd: 4.82, unpriced: false, ...o })
const sess = { id: 's1', agent: 'claude', started_at: '2026-11-05T10:00:00Z', last_event_at: '2026-11-05T10:40:00Z', ended_at: null, usage: tot(), unpriced: false }
const detail = {
  id: 1, key: 'NAC-42', title: 'Migrar autenticação', goal: 'Trocar por OAuth2', done_when: '', how_to_test: '',
  where_stopped: 'Falta store.Delete', where_stopped_at: null, branch: 'feat/oauth2', project_id: 1, column: 'revisao',
  content: {
    decisions: [{ id: 1, what: 'PKCE obrigatório', why: 'IdP exige' }],
    files: [{ id: 1, path: 'internal/auth/oauth.go', lines: 'todo', why: 'Fluxo' }],
    todos: [{ id: 5, card_id: 1, text: 'Apagar token', done: false }],
    questions: [{ id: 9, card_id: 1, text: 'Revogar tudo?', answer: '' }],
  },
  sessions: [sess], totals: tot(), live: sess,
  readiness: { met: 4, total: 5, ready: false, items: [{ rule: 'a', label: 'Onde parei preenchido', ok: true }, { rule: 'b', label: 'Todos feitos', ok: false }] },
  touched_files: [{ path: 'a.go', kind: 'editado', count: 2, last_at: '' }, { path: 'b.go', kind: 'lido', count: 1, last_at: '' }],
} as unknown as CardDetail

type Reply = unknown
function mock(routes: Record<string, Reply>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{"error":"não encontrado"}', { status: 404 })
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
const prompts = [
  { slug: 'a', name: 'Revisar', is_default_handoff: false },
  { slug: 'retomar', name: 'Retomar card', is_default_handoff: true },
]
const base = { '/api/agents': [{ agent: 'claude', installable: true, status_line: true, detection: { found: true, path: '/usr/bin/claude', version: '2', checks: [] } }], '/api/settings': { currency: 'USD', brl_rate: 0 }, '/api/limits': [{ agent: 'claude', card_usd: 12, daily_usd: 0, warn_pct: 80, action: 'warn' }], '/api/projects': [{ id: 1, name: 'portal-api' }], '/api/prompts': prompts }
function wrap(ui: React.ReactNode, path = '/cards/NAC-42') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}><Routes><Route path="/cards/:key" element={ui} /></Routes></MemoryRouter>
    </QueryClientProvider>,
  )
}
afterEach(() => vi.unstubAllGlobals())

describe('CardPage', () => {
  it('mostra o documento de handoff e o painel Continuar habilitado', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail })
    wrap(<CardPage />)
    expect(await screen.findByRole('heading', { name: 'Onde parei' })).toBeInTheDocument()
    expect(screen.getByText('4 de 5')).toBeInTheDocument()
    expect(screen.getByText('Falta store.Delete')).toBeInTheDocument()
    expect(screen.getByText('Total do card')).toBeInTheDocument()
    expect(screen.getByText('ao vivo')).toBeInTheDocument()
    expect(screen.getByText('nobre resume NAC-42')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' })).toBeEnabled()
    expect(screen.getByText(/2 arquivos tocados/)).toBeInTheDocument()
    expect(screen.queryByText('a.go')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ver arquivos tocados' }))
    expect(screen.getByText('a.go')).toBeInTheDocument()
  })

  it('sessão filha mostra a marca "outra janela"; a principal não', async () => {
    const child = { ...sess, id: 's2', parent_session_id: 's1' }
    mock({ ...base, '/api/cards/NAC-42': { ...detail, sessions: [{ ...sess, parent_session_id: '' }, child] } })
    wrap(<CardPage />)
    expect(await screen.findAllByText('outra janela')).toHaveLength(1)
  })

  it('processo da sessão morto: mostra "processo encerrado há N min", não "ao vivo"', async () => {
    const dead = { ...sess, card_id: 1, pid_dead_at: new Date(Date.now() - 4 * 60_000).toISOString() }
    mock({ ...base, '/api/cards/NAC-42': { ...detail, column: 'em_andamento', sessions: [dead], live: null } })
    wrap(<CardPage />)
    expect(await screen.findByText('processo encerrado há 4 min')).toBeInTheDocument()
    expect(screen.queryByText(/ao vivo/i)).not.toBeInTheDocument()
  })

  it('card inexistente mostra Card não encontrado', async () => {
    mock({ ...base })
    wrap(<CardPage />)
    expect(await screen.findByText('Card não encontrado')).toBeInTheDocument()
  })

  it('edita onde parei via PATCH', async () => {
    const fn = mock({ ...base, '/api/cards/NAC-42': detail, 'PATCH /api/cards/NAC-42': {} })
    wrap(<CardPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar onde parei' }))
    fireEvent.change(screen.getByLabelText('onde parei'), { target: { value: 'novo texto' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar onde parei' }))
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/cards/NAC-42')).toEqual([{ where_stopped: 'novo texto' }]))
  })

  it('adiciona e marca todo, responde pergunta, aprova', async () => {
    const fn = mock({
      ...base, '/api/cards/NAC-42': detail,
      'POST /api/cards/NAC-42/todos': {}, 'PATCH /api/todos/5': {}, 'PATCH /api/questions/9': {}, 'PATCH /api/cards/NAC-42': {},
    })
    wrap(<CardPage />)
    fireEvent.change(await screen.findByLabelText('Novo item'), { target: { value: 'Docs' } })
    fireEvent.click(screen.getByRole('button', { name: 'Adicionar item' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/todos')).toEqual([{ text: 'Docs' }]))
    fireEvent.click(screen.getByLabelText('Apagar token'))
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/todos/5')).toEqual([{ done: true }]))
    fireEvent.change(screen.getByLabelText('Responder pergunta: Revogar tudo?'), { target: { value: 'sim' } })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/questions/9')).toEqual([{ answer: 'sim' }]))
    fireEvent.click(screen.getByRole('button', { name: 'Aprovar e concluir' }))
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/cards/NAC-42')).toEqual([{ column: 'concluido' }]))
  })

  it('mostra as marcas do card', async () => {
    mock({ ...base, '/api/cards/NAC-42': { ...detail, column: 'pausado', pause_reason: 'limite de custo', handoff_incomplete: true } })
    wrap(<CardPage />)
    expect(await screen.findByText('pausado por limite de custo')).toBeInTheDocument()
    expect(screen.getByText('handoff incompleto')).toBeInTheDocument()
  })

  it('sem a marca de handoff incompleto quando o card já está pronto', async () => {
    mock({ ...base, '/api/cards/NAC-42': { ...detail, column: 'pausado', handoff_incomplete: true, readiness: { ...detail.readiness, ready: true } } })
    wrap(<CardPage />)
    await screen.findByRole('heading', { name: 'Onde parei' })
    expect(screen.queryByText('handoff incompleto')).not.toBeInTheDocument()
    expect(screen.queryByText('pausado por limite de custo')).not.toBeInTheDocument()
  })

  it('salva o bloqueio do card', async () => {
    const fn = mock({ ...base, '/api/cards/NAC-42': { ...detail, blocked_by: '' }, 'PATCH /api/cards/NAC-42': {} })
    wrap(<CardPage />)
    fireEvent.change(await screen.findByLabelText('Bloqueado por'), { target: { value: 'NAC-4' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar bloqueio' }))
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/cards/NAC-42')).toEqual([{ blocked_by: 'NAC-4' }]))
  })

  it('mostra o erro do daemon ao bloquear por chave inexistente', async () => {
    mock({ ...base, '/api/cards/NAC-42': { ...detail, blocked_by: '' }, 'PATCH /api/cards/NAC-42': { status: 404, body: { error: 'card NAC-99 não encontrado' } } })
    wrap(<CardPage />)
    fireEvent.change(await screen.findByLabelText('Bloqueado por'), { target: { value: 'NAC-99' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar bloqueio' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('card NAC-99 não encontrado')
  })

  it('mostra o pacote de retomada sob demanda', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail, '/api/cards/NAC-42/resume': { markdown: '# NAC-42 pacote' } })
    wrap(<CardPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ver o pacote de retomada' }))
    expect(await screen.findByText('# NAC-42 pacote')).toBeInTheDocument()
  })
})

describe('ContinuePanel', () => {
  it('mostra a prévia do texto que o Continuar envia, para o prompt escolhido', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue/preview': { text: 'Você está continuando o card NAC-42.', prompt_slug: 'retomar' } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue/preview')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Ver o que será enviado' }))
    expect(await screen.findByText('Você está continuando o card NAC-42.')).toBeInTheDocument()
    expect(screen.getByText(/O pacote de retomada não vai neste texto/)).toBeInTheDocument()
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue/preview')).toEqual([{ prompt_slug: 'retomar' }])
    fireEvent.change(screen.getByRole('combobox', { name: 'Prompt inicial' }), { target: { value: 'nenhum' } })
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue/preview')).toContainEqual({ prompt_slug: 'nenhum' }))
    fireEvent.click(screen.getByRole('button', { name: 'Ocultar o que será enviado' }))
    expect(screen.queryByText('Você está continuando o card NAC-42.')).not.toBeInTheDocument()
  })

  it('sem prompt de handoff padrão, a seleção mostra o texto padrão do Cockpit (o que é enviado)', async () => {
    const fn = mock({ ...base, '/api/prompts': [{ slug: 'a', name: 'Revisar', is_default_handoff: false }], 'POST /api/cards/NAC-42/continue': { ok: true } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Revisar' })
    expect(screen.getByRole('combobox', { name: 'Prompt inicial' })).toHaveValue('')
    expect((screen.getByRole('option', { name: 'Texto padrão do Cockpit' }) as HTMLOptionElement).selected).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toHaveLength(1))
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[0].prompt_slug).toBe('')
  })

  it('envia continue com prompt padrão e mostra o 409 em linha', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue': { status: 409, body: { error: 'NAC-42 já tem uma sessão viva.' } } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('NAC-42 já tem uma sessão viva.')
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toEqual([{ agent: 'claude', mode: 'platform', prompt_slug: 'retomar' }])
  })

  it('oferece Forçar mesmo assim após o 409 de sessão viva', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue': { status: 409, body: { error: 'NAC-42 está com uma sessão viva de Claude Code. Encerre-a ou use forçar.', code: 'live_session' } } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Forçar mesmo assim' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toHaveLength(2))
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[1]).toMatchObject({ force: true })
  })

  it('mostra o modo padrão da Configuração na opção Padrão da plataforma', async () => {
    mock({ ...base, '/api/settings': { currency: 'USD', brl_rate: 0, launch_mode: 'background' } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    expect(await screen.findByRole('option', { name: 'Padrão da plataforma (Segundo plano, com saída na interface)' })).toBeInTheDocument()
  })

  it('permite escolher nenhum prompt', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue': { ok: true } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    fireEvent.change(screen.getByLabelText('Prompt inicial'), { target: { value: 'nenhum' } })
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toHaveLength(1))
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[0].prompt_slug).toBe('nenhum')
  })

  it('Continuar em segundo plano envia mode=background e mostra o terminal do card', async () => {
    const term = { id: 'tt1', card_key: 'NAC-42', agent: 'claude', origin: 'plataforma', title: '', pid: 1, started_at: '2026-11-05T10:00:00Z', ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 0 }
    const fn = mock({ ...base, '/api/cards/NAC-42': detail, '/api/terminals': [term], 'POST /api/cards/NAC-42/continue': { terminal_id: 'tt1' } })
    wrap(<CardPage />)
    expect(await screen.findByRole('heading', { name: 'Terminal' })).toBeInTheDocument()
    expect(await screen.findByTestId('tv')).toHaveTextContent('tt1 (somente assistir)') // painel do card nunca toma o PTY
    fireEvent.change(await screen.findByLabelText('Abrir em'), { target: { value: 'background' } })
    fireEvent.click(screen.getByRole('button', { name: /Iniciar Claude Code neste card/ }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[0]).toMatchObject({ mode: 'background' }))
  })

  it('modo da plataforma que devolve terminal_id foca o painel Terminal', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    const term = { id: 'tt1', card_key: 'NAC-42', agent: 'claude', origin: 'plataforma', title: '', pid: 1, started_at: '2026-11-05T10:00:00Z', ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 0 }
    mock({ ...base, '/api/cards/NAC-42': detail, '/api/terminals': [term], 'POST /api/cards/NAC-42/continue': { terminal_id: 'tt1' } })
    wrap(<CardPage />)
    fireEvent.click(await screen.findByRole('button', { name: /Iniciar Claude Code neste card/ }))
    expect(await screen.findByText('Agente iniciado em segundo plano — terminal abaixo.')).toBeInTheDocument()
    await waitFor(() => expect(scroll).toHaveBeenCalled())
  })

  it('card project select limited to family', async () => {
    const mk = (id: number, name: string, parent_id: number | null) => ({ id, name, path: `/w/${name}`, capture: true, base_branch: 'main', parent_id })
    const fn = mock({
      ...base, '/api/cards/NAC-42': { ...detail, project_id: 2 },
      '/api/projects': [mk(1, 'loja', null), mk(2, 'web', 1), mk(3, 'api', 1), mk(4, 'outro', null)],
      'PATCH /api/cards/NAC-42': {},
    })
    wrap(<CardPage />)
    const sel = await screen.findByLabelText('Projeto')
    expect(Array.from((sel as HTMLSelectElement).options).map((o) => o.text)).toEqual(['loja', 'loja › web', 'loja › api'])
    fireEvent.change(sel, { target: { value: '3' } })
    await waitFor(() => expect(bodies(fn, 'PATCH', '/api/cards/NAC-42')).toEqual([{ project_id: 3 }]))
  })

  it('card project select leaves out pathless groups', async () => {
    const mk = (id: number, name: string, path: string, parent_id: number | null) => ({ id, name, path, capture: true, base_branch: 'main', parent_id, kind: path === '' ? 'group' : 'repo' })
    mock({
      ...base, '/api/cards/NAC-42': { ...detail, project_id: 2 },
      '/api/projects': [mk(1, 'loja', '', null), mk(2, 'web', '/w/web', 1), mk(3, 'api', '/w/api', 1)],
    })
    wrap(<CardPage />)
    const sel = await screen.findByLabelText('Projeto')
    expect(Array.from((sel as HTMLSelectElement).options).map((o) => o.text)).toEqual(['loja › web', 'loja › api'])
  })

  it('group with folder is a valid card target', async () => {
    const mk = (id: number, name: string, path: string, parent_id: number | null, kind: string) => ({ id, name, path, capture: true, base_branch: '', parent_id, kind })
    mock({
      ...base, '/api/cards/NAC-42': { ...detail, project_id: 1 },
      '/api/projects': [mk(1, 'loja', '/w/loja', null, 'group'), mk(2, 'web', '/w/loja/web', 1, 'repo')],
    })
    wrap(<CardPage />)
    const sel = await screen.findByLabelText('Projeto')
    expect(Array.from((sel as HTMLSelectElement).options).map((o) => o.text)).toEqual(['loja', 'loja › web'])
  })
})

describe('CardPage contexto', () => {
  const ctx = { tokens: 620000, model: 'm', window_tokens: 1000000, window_estimated: false, pct: 62, growth_per_turn: 12000, turns_to_limit: 25, at_limit: false, turns_since_compact: 4, compactions: 1, cache_hit_pct: 90, recent_rewrite: null, ttl: '5m', idle_sec: 30, idle_over_ttl: false, top_outputs: [] }
  const live = (key: string) => [{ id: 's1', card_key: key, card_title: 'x', tokens: 620000, window_tokens: 1000000, pct: 62, over80: false, rewrite: false, idle_over_ttl: false }]

  it('mostra o contexto das sessões vivas do card e recomeça limpo', async () => {
    const fn = mock({ ...base, '/api/cards/NAC-42': detail, '/api/context/live': live('NAC-42'), '/api/sessions/s1/context': ctx, 'POST /api/sessions/s1/restart-clean': { ok: true, terminal_id: 't1' } })
    wrap(<CardPage />)
    expect(await screen.findByRole('heading', { name: 'Contexto' })).toBeInTheDocument()
    expect(await screen.findByText('Cache aproveitado')).toBeInTheDocument()
    expect(screen.getByText('62% · 620 mil / 1 milhão')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Recomeçar limpo' }))
    const sec = await screen.findByRole('region', { name: 'Recomeçar limpo' })
    expect(sec).toBeInTheDocument()
    // where_stopped_at é nulo no fixture: o registro está velho e o texto é exigido.
    fireEvent.click(sec.querySelector('button')!)
    await screen.findByText('Terminal novo aberto.')
    expect(bodies(fn, 'POST', '/api/sessions/s1/restart-clean')).toEqual([{ where_stopped: 'Falta store.Delete' }])
  })

  it('sem sessão viva do card, não há seção Contexto', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail, '/api/context/live': live('NAC-99') })
    wrap(<CardPage />)
    await screen.findByRole('heading', { name: 'Onde parei' })
    expect(screen.queryByRole('heading', { name: 'Contexto' })).not.toBeInTheDocument()
  })
})

describe('CardPage plano', () => {
  it('mostra a seção Plano com caminho e tarefas', async () => {
    const plan = { path: 'docs/plan.md', tasks: [
      { n: 1, title: 'API', status: 'concluida', session_id: 's1', updated_at: '2026-11-03T12:00:00Z' },
      { n: 2, title: 'Filtros', status: 'em_revisao', session_id: 's1', updated_at: '2026-11-05T12:00:00Z' },
    ] }
    mock({ ...base, '/api/cards/NAC-42': { ...detail, content: { ...detail.content, plan } } })
    wrap(<CardPage />)
    const sec = await screen.findByRole('region', { name: 'Plano' })
    const path = within(sec).getByText('docs/plan.md')
    expect(path).toHaveClass('mono')
    const row = within(sec).getByText('Filtros').closest('li')!
    expect(row).toHaveTextContent('2')
    expect(row).toHaveTextContent('em revisão')
    expect(row).toHaveTextContent('05/11')
    expect(within(sec).getByText('API').closest('li')).toHaveTextContent('concluída')
  })
  it('sem plano', async () => {
    mock({ ...base, '/api/cards/NAC-42': { ...detail, content: { ...detail.content, plan: null } } })
    wrap(<CardPage />)
    expect(await screen.findByText('Sem plano registrado. O agente registra com nobre plan.')).toBeInTheDocument()
  })
})
