import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CardPage } from './CardPage'
import { ContinuePanel } from './card/ContinuePanel'
import type { CardDetail } from '../api/types'

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
const base = { '/api/settings': { currency: 'USD', brl_rate: 0 }, '/api/limits': [{ agent: 'claude', card_usd: 12, daily_usd: 0, warn_pct: 80, action: 'warn' }], '/api/projects': [{ id: 1, name: 'portal-api' }], '/api/prompts': prompts }
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

  it('mostra o pacote de retomada sob demanda', async () => {
    mock({ ...base, '/api/cards/NAC-42': detail, '/api/cards/NAC-42/resume': { markdown: '# NAC-42 pacote' } })
    wrap(<CardPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Ver o pacote de retomada' }))
    expect(await screen.findByText('# NAC-42 pacote')).toBeInTheDocument()
  })
})

describe('ContinuePanel', () => {
  it('envia continue com prompt padrão e mostra o 409 em linha', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue': { status: 409, body: { error: 'NAC-42 já tem uma sessão viva.' } } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('NAC-42 já tem uma sessão viva.')
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toEqual([{ agent: 'claude', mode: 'platform', prompt_slug: 'retomar' }])
  })

  it('oferece Forçar mesmo assim após o 409 de sessão viva', async () => {
    const fn = mock({ ...base, 'POST /api/cards/NAC-42/continue': { status: 409, body: { error: 'NAC-42 está com uma sessão viva de Claude Code. Encerre-a ou use forçar.' } } })
    wrap(<ContinuePanel cardKey="NAC-42" enabled />)
    await screen.findByRole('option', { name: 'Retomar card (padrão)' })
    fireEvent.click(screen.getByRole('button', { name: 'Iniciar Claude Code neste card' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Forçar mesmo assim' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')).toHaveLength(2))
    expect(bodies(fn, 'POST', '/api/cards/NAC-42/continue')[1]).toMatchObject({ force: true })
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
})
