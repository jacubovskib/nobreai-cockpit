import { fireEvent, render, screen, within } from '@testing-library/react'
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
  it('o link do card leva o projeto escolhido', async () => {
    mock(routes)
    wrap(<Board />, '/board?projeto=2')
    const link = await screen.findByRole('link', { name: 'Card do web' })
    expect(link).toHaveAttribute('href', '/cards/NAC-3?projeto=2')
  })
  it('mostra "branch integrada" em card não concluído', async () => {
    mock({
      ...routes,
      '/api/cards': [
        card({ id: 1, key: 'NAC-1', title: 'Card ativo', column: 'em_andamento', branch_merged_at: ago(1) }),
        card({ id: 2, key: 'NAC-2', title: 'Card pronto', column: 'concluido', branch_merged_at: ago(1) }),
      ],
    })
    wrap(<Board />, '/board')
    await screen.findByText('Card ativo')
    expect(screen.getByRole('article', { name: /NAC-1/ })).toHaveTextContent('branch integrada')
    expect(screen.getByRole('article', { name: /NAC-2/ })).not.toHaveTextContent('branch integrada')
  })
})

describe('Board com família', () => {
  const fam = {
    '/api/projects': [
      { id: 1, name: 'loja', path: '/w/loja', capture: true, parent_id: null },
      { id: 2, name: 'web', path: '/w/loja/web', capture: true, parent_id: 1 },
    ],
    '/api/cards': [
      card({ id: 1, key: 'NAC-1', title: 'Card do pai', project_id: 1 }),
      card({ id: 2, key: 'NAC-2', title: 'Card do app', project_id: 2 }),
    ],
  }
  it('parent filter shows children cards', async () => {
    mock(fam)
    wrap(<Board />, '/board?projeto=1')
    expect(await screen.findByText('Card do pai')).toBeInTheDocument()
    expect(screen.getByText('Card do app')).toBeInTheDocument()
    expect(screen.getByText('loja › web')).toBeInTheDocument()
  })
  it('child filter hides parent cards', async () => {
    mock(fam)
    wrap(<Board />, '/board?projeto=2')
    expect(await screen.findByText('Card do app')).toBeInTheDocument()
    expect(screen.queryByText('Card do pai')).toBeNull()
  })
})

describe('Board marcas e sinais', () => {
  it('mostra as marcas do card', async () => {
    mock({
      ...routes,
      '/api/cards': [
        card({ id: 1, key: 'NAC-1', title: 'Limite', column: 'pausado', pause_reason: 'limite de custo' }),
        card({ id: 2, key: 'NAC-2', title: 'Incompleto', column: 'pausado', handoff_incomplete: true }),
        card({ id: 3, key: 'NAC-3', title: 'Completo', column: 'pausado', handoff_incomplete: true, readiness: { met: 5, total: 5, ready: true } }),
      ],
    })
    wrap(<Board />, '/board')
    await screen.findByText('Limite')
    expect(screen.getAllByText('pausado por limite de custo')).toHaveLength(1)
    expect(screen.getAllByText('handoff incompleto')).toHaveLength(1)
  })

  it('mostra os sinais de pausa', async () => {
    mock({
      ...routes,
      '/api/cards': [
        card({ id: 1, key: 'NAC-1', title: 'A', column: 'pausado', signal: 'bloqueado', signal_since: ago(2), blocked_by: 'NAC-4' }),
        card({ id: 2, key: 'NAC-2', title: 'B', column: 'pausado', signal: 'pronto_para_retomar', signal_since: ago(2), blocked_by: 'NAC-4' }),
        card({ id: 3, key: 'NAC-3', title: 'C', column: 'pausado', signal: 'aguardando_voce', signal_since: ago(3) }),
      ],
    })
    wrap(<Board />, '/board')
    expect(await screen.findByText('bloqueado por NAC-4')).toBeInTheDocument()
    expect(screen.getByText('pronto para retomar (NAC-4 concluído)')).toBeInTheDocument()
    expect(screen.getByText(/aguardando você há/)).toBeInTheDocument()
  })
})

describe('Board tipos e hierarquia', () => {
  const t = (cost: number) => ({ cost_usd: cost, unpriced: false, credits: 0 })
  const base = { type: 'tarefa', parent_id: null, priority: 'media', points: null, labels: [], parent_key: '', parent_title: '', children: null, tree_totals: null, waiting_on_you: false, waiting_reason: '' }
  const tree = {
    '/api/projects': routes['/api/projects'],
    '/api/cards': [
      card({ ...base, id: 10, key: 'NAC-10', title: 'Checkout novo', type: 'epico', column: 'em_andamento', totals: t(0), tree_totals: t(12.5), children: { total: 7, done: 4 }, readiness: { met: 0, total: 0, ready: false, items: [] } }),
      card({ ...base, id: 11, key: 'NAC-11', title: 'Tela de pagamento', parent_id: 10, parent_key: 'NAC-10', parent_title: 'Checkout novo', points: 3, priority: 'alta', labels: ['frontend'], waiting_on_you: true, waiting_reason: 'revisão' }),
      card({ ...base, id: 12, key: 'NAC-12', title: 'Tarefa solta' }),
    ],
  }
  it('épico mostra progresso e custo da árvore, sem handoff nem aviso de sessão', async () => {
    mock(tree)
    wrap(<Board />, '/board')
    const epic = await screen.findByRole('article', { name: /NAC-10/ })
    expect(epic).toHaveTextContent('4/7')
    expect(epic).toHaveTextContent('12,50')
    expect(epic).not.toHaveTextContent('para handoff')
    expect(epic).not.toHaveTextContent('sem sessão')
  })
  it('filha mostra o chip do épico, pontos, prioridade e labels', async () => {
    mock(tree)
    wrap(<Board />, '/board')
    const child = await screen.findByRole('article', { name: /NAC-11/ })
    expect(within(child).getByRole('link', { name: '◆ Checkout novo' })).toHaveAttribute('href', '/cards/NAC-10')
    expect(child).toHaveTextContent('3 pts')
    expect(child).toHaveTextContent('Alta')
    expect(child).toHaveTextContent('frontend')
  })
  it('tarefa média sem pai fica como antes', async () => {
    mock(tree)
    wrap(<Board />, '/board')
    const plain = await screen.findByRole('article', { name: /NAC-12/ })
    expect(plain).toHaveTextContent('0/5 para handoff')
    expect(plain).not.toHaveTextContent('pts')
    expect(plain).not.toHaveTextContent('● Tarefa')
  })
  it('Esperando você mostra a contagem e filtra, e o filtro de épico vem da URL', async () => {
    mock(tree)
    wrap(<Board />, '/board')
    const btn = await screen.findByRole('button', { name: 'Esperando você · 1' })
    fireEvent.click(btn)
    expect(screen.queryByText('Tarefa solta')).toBeNull()
    expect(screen.getByText('Tela de pagamento')).toBeInTheDocument()
  })
  it('?epico=none esconde épico e filhas', async () => {
    mock(tree)
    wrap(<Board />, '/board?epico=none')
    expect(await screen.findByText('Tarefa solta')).toBeInTheDocument()
    expect(screen.queryByText('Checkout novo')).toBeNull()
    expect(screen.queryByText('Tela de pagamento')).toBeNull()
  })
  it('épico de um grupo aparece quando a filha de um sub-projeto está visível', async () => {
    mock({
      '/api/projects': [
        { id: 1, name: 'loja', path: '', capture: true, parent_id: null, kind: 'group' },
        { id: 2, name: 'web', path: '/w/loja/web', capture: true, parent_id: 1 },
      ],
      '/api/cards': [
        card({ ...base, id: 10, key: 'NAC-10', title: 'Épico do grupo', type: 'epico', project_id: 1, children: { total: 1, done: 0 }, tree_totals: t(1) }),
        card({ ...base, id: 11, key: 'NAC-11', title: 'Filha do web', project_id: 2, parent_id: 10, parent_key: 'NAC-10', parent_title: 'Épico do grupo' }),
      ],
    })
    wrap(<Board />, '/board?projeto=2')
    expect(await screen.findByRole('article', { name: /NAC-10/ })).toBeInTheDocument()
  })
  it('o select de épico oferece o épico de outro projeto cujas filhas estão visíveis', async () => {
    mock({
      '/api/projects': [
        { id: 1, name: 'api', path: '/w/api', capture: true, parent_id: null },
        { id: 2, name: 'web', path: '/w/web', capture: true, parent_id: null },
      ],
      '/api/cards': [
        card({ ...base, id: 10, key: 'NAC-10', title: 'Épico da api', type: 'epico', project_id: 1, children: { total: 2, done: 0 }, tree_totals: t(1) }),
        card({ ...base, id: 11, key: 'NAC-11', title: 'Filha do web', project_id: 2, parent_id: 10, parent_key: 'NAC-10', parent_title: 'Épico da api' }),
        card({ ...base, id: 12, key: 'NAC-12', title: 'Solta do web', project_id: 2 }),
      ],
    })
    wrap(<Board />, '/board?projeto=2')
    expect(await screen.findByText('Solta do web')).toBeInTheDocument()
    const sel = screen.getByLabelText('Filtrar por épico') as HTMLSelectElement
    expect(within(sel).getByRole('option', { name: 'NAC-10 Épico da api' })).toBeInTheDocument()
    fireEvent.change(sel, { target: { value: 'NAC-10' } })
    expect(await screen.findByText('Filha do web')).toBeInTheDocument()
    expect(screen.queryByText('Solta do web')).toBeNull()
  })
  it('?epico= fora das opções mostra uma opção com a chave', async () => {
    mock(tree)
    wrap(<Board />, '/board?epico=NAC-99')
    const sel = (await screen.findByLabelText('Filtrar por épico')) as HTMLSelectElement
    expect(sel.value).toBe('NAC-99')
    expect(within(sel).getByRole('option', { name: 'NAC-99' })).toBeInTheDocument()
  })
})
