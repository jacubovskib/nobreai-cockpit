import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  it('tarefa média sem pai, no backlog e sem sessão, mostra só chave e título', async () => {
    mock(tree)
    wrap(<Board />, '/board')
    const plain = await screen.findByRole('article', { name: /NAC-12/ })
    expect(plain).toHaveTextContent('NAC-12')
    expect(plain).toHaveTextContent('Tarefa solta')
    expect(plain).not.toHaveTextContent(/\d sess/)
    expect(plain).not.toHaveTextContent('US$')
    expect(plain).not.toHaveTextContent(/handoff|contexto/)
    expect(within(plain).queryByRole('img')).toBeNull()
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

describe('Board enxuto (NAC-61)', () => {
  const ready = { met: 5, total: 5, ready: true }
  const long = 'Versão nova instalada e daemon reiniciado. '.repeat(8).trim()
  const done = (n: number) => card({ id: 20 + n, key: `NAC-${20 + n}`, title: `Feito ${n}`, column: 'concluido', updated_at: `2026-05-0${n}T00:00:00Z` })
  const lean = {
    '/api/projects': routes['/api/projects'],
    '/api/cards': [
      card({ id: 1, key: 'NAC-1', title: 'Card cru', column: 'backlog', type: 'bug', readiness: { met: 1, total: 5, ready: false } }),
      card({ id: 2, key: 'NAC-2', title: 'Rodando agora', column: 'em_andamento', live: true, agent: 'claude', session_count: 1, readiness: { met: 2, total: 5, ready: false } }),
      card({ id: 3, key: 'NAC-3', title: 'Pausado pronto', column: 'pausado', agent: 'claude', session_count: 2, readiness: ready, where_stopped: long }),
      card({ id: 4, key: 'NAC-4', title: 'Pausado parcial', column: 'pausado', agent: 'claude', session_count: 1, readiness: { met: 3, total: 5, ready: false } }),
      card({ id: 5, key: 'NAC-5', title: 'Em revisão velha', column: 'revisao', session_count: 1, readiness: ready, signal: 'aguardando_revisao', signal_since: ago(73) }),
      card({ id: 6, key: 'NAC-6', title: 'Em revisão nova', column: 'revisao', session_count: 1, readiness: ready }),
      ...[1, 2, 3, 4, 5].map(done),
    ],
  }
  const calls = (method: string) => (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls
    .filter(([, i]) => (i?.method ?? 'GET') === method).map(([u, i]) => [u, i?.body ? JSON.parse(i.body) : undefined])

  it('"Onde parei" fica cortado em 2 linhas, com o texto completo na dica', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const where = await screen.findByText(`Onde parei: ${long}`)
    expect(where).toHaveAttribute('title', long)
    expect(where.style.overflow).toBe('hidden')
    expect(where.style.webkitLineClamp ?? where.style.getPropertyValue('-webkit-line-clamp')).toBe('2')
  })

  it('não há seletor "Mover para…" no card; o botão ⋯ abre as colunas e move', async () => {
    mock({ ...lean, '/api/cards/NAC-4': {} })
    wrap(<Board />, '/board')
    const art = await screen.findByRole('article', { name: /NAC-4/ })
    expect(screen.queryByRole('combobox', { name: /Mover NAC-/ })).toBeNull()
    expect(art).not.toHaveTextContent('Mover para')
    const btn = within(art).getByRole('button', { name: 'Mover NAC-4' })
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    const group = within(art).getByRole('group', { name: 'Mover NAC-4 para' })
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual(['Backlog', 'Em andamento', 'Revisão', 'Concluído'])
    fireEvent.click(within(group).getByRole('button', { name: 'Revisão' }))
    await waitFor(() => expect(calls('PATCH')).toEqual([['/api/cards/NAC-4', { column: 'revisao' }]]))
    expect(within(art).queryByRole('group', { name: 'Mover NAC-4 para' })).toBeNull()
  })

  it('Esc fecha as opções de mover', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const art = await screen.findByRole('article', { name: /NAC-4/ })
    const btn = within(art).getByRole('button', { name: 'Mover NAC-4' })
    fireEvent.click(btn)
    fireEvent.keyDown(btn, { key: 'Escape' })
    expect(within(art).queryByRole('group', { name: 'Mover NAC-4 para' })).toBeNull()
  })

  it('ação principal: Continuar nos pausados (painel de continuar) e Revisar e aprovar na revisão', async () => {
    mock(lean)
    wrap(<Board />, '/board?projeto=1')
    await screen.findByText('Pausado pronto')
    expect(screen.getByRole('link', { name: 'Continuar NAC-3' })).toHaveAttribute('href', '/cards/NAC-3?projeto=1#cont-t')
    expect(screen.getByRole('link', { name: 'Continuar NAC-4' })).toHaveTextContent('Continuar')
    expect(screen.getByRole('link', { name: 'Revisar e aprovar NAC-5' })).toHaveAttribute('href', '/cards/NAC-5?projeto=1')
    expect(screen.getAllByRole('link', { name: /^Continuar / })).toHaveLength(2)
    expect(screen.getAllByRole('link', { name: /^Revisar e aprovar / })).toHaveLength(2)
  })

  it('faixa do topo mostra o que espera por você e leva à coluna', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    mock(lean)
    wrap(<Board />, '/board')
    const strip = await screen.findByRole('group', { name: 'O que espera por você' })
    const names = within(strip).getAllByRole('button').map((b) => b.textContent)
    expect(names).toEqual(['2 aguardando sua revisão· a mais antiga há 3 dias', '1 pausado pronto para retomar', '1 agente ao vivo'])
    fireEvent.click(within(strip).getByRole('button', { name: /aguardando sua revisão/ }))
    expect(scroll).toHaveBeenCalledTimes(1)
    expect(scroll.mock.contexts[0]).toBe(screen.getByRole('region', { name: 'Revisão' }))
    expect(document.activeElement).toBe(screen.getByRole('region', { name: 'Revisão' }))
    fireEvent.click(within(strip).getByRole('button', { name: /pronto para retomar/ }))
    expect(scroll.mock.contexts[1]).toBe(screen.getByRole('region', { name: 'Pausado / Handoff' }))
    fireEvent.click(within(strip).getByRole('button', { name: /ao vivo/ }))
    expect(scroll.mock.contexts[2]).toBe(screen.getByRole('region', { name: 'Em andamento' }))
  })

  it('faixa omite item zerado, a idade sem dado, e some sem itens', async () => {
    mock({ ...lean, '/api/cards': [lean['/api/cards'][0], lean['/api/cards'][5]] })
    const { unmount } = wrap(<Board />, '/board')
    const strip = await screen.findByRole('group', { name: 'O que espera por você' })
    expect(within(strip).getAllByRole('button').map((b) => b.textContent)).toEqual(['1 aguardando sua revisão'])
    unmount()
    mock({ ...lean, '/api/cards': [lean['/api/cards'][0]] })
    wrap(<Board />, '/board')
    await screen.findByText('Card cru')
    expect(screen.queryByRole('group', { name: 'O que espera por você' })).toBeNull()
  })

  it('backlog sem sessão: chave, tipo e título, sem sessões, custo nem handoff', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const art = await screen.findByRole('article', { name: /NAC-1 / })
    expect(art).toHaveTextContent('NAC-1')
    expect(art).toHaveTextContent('▲ Bug')
    expect(art).toHaveTextContent('Card cru')
    expect(art).not.toHaveTextContent(/\d sess/)
    expect(art).not.toHaveTextContent('US$')
    expect(art).not.toHaveTextContent(/handoff|contexto/)
  })

  it('a chave do card nunca quebra de linha', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const art = await screen.findByRole('article', { name: /NAC-3/ })
    const key = within(art).getByTestId('card-key')
    expect(key).toHaveTextContent('NAC-3')
    expect(key.style.whiteSpace).toBe('nowrap')
  })

  it('indicador de handoff só quando informa algo', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const text = async (k: string) => (await screen.findByRole('article', { name: new RegExp(`${k} `) }))
    expect(await text('NAC-3')).toHaveTextContent('pronto para retomar')
    expect(within(await text('NAC-3')).queryByRole('img')).toBeNull()
    expect(await text('NAC-4')).toHaveTextContent('contexto 3/5')
    expect(within(await text('NAC-4')).getByRole('img', { name: '3/5 para handoff' })).toBeInTheDocument()
    expect(await text('NAC-2')).toHaveTextContent('contexto 2/5')
    for (const k of ['NAC-1', 'NAC-5', 'NAC-6', 'NAC-25']) {
      expect(await text(k)).not.toHaveTextContent(/contexto|pronto para|handoff/)
      expect(within(await text(k)).queryByRole('img')).toBeNull()
    }
  })

  it('Concluído mostra os 3 mais recentes e expande no lugar', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    const col = await screen.findByRole('region', { name: 'Concluído' })
    await within(col).findByText('Feito 5')
    const titles = () => within(col).getAllByRole('article').map((a) => a.getAttribute('aria-label'))
    expect(titles()).toEqual(['NAC-25 Feito 5', 'NAC-24 Feito 4', 'NAC-23 Feito 3'])
    expect(within(col).getByText('5')).toBeInTheDocument() // o contador segue com o total
    const more = within(col).getByRole('button', { name: 'Ver os 5 concluídos' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(more)
    expect(titles()).toHaveLength(5)
    fireEvent.click(within(col).getByRole('button', { name: 'Mostrar só os 3 mais recentes' }))
    expect(titles()).toHaveLength(3)
  })

  it('com até 3 concluídos não há link para ver mais', async () => {
    mock({ ...lean, '/api/cards': [1, 2, 3].map(done) })
    wrap(<Board />, '/board')
    await screen.findByText('Feito 3')
    expect(screen.queryByRole('button', { name: /Ver os/ })).toBeNull()
  })

  it('a descrição da coluna vira dica do nome, fora do texto', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    await screen.findByText('Card cru')
    expect(screen.getByRole('heading', { name: 'Revisão' })).toHaveAttribute('title', 'O agente declarou pronto; aguarda você')
    expect(screen.getByRole('heading', { name: 'Backlog' })).toHaveAttribute('title', 'Criado por você ou por um agente, sem sessão ainda')
    expect(screen.queryByText('O agente declarou pronto; aguarda você')).toBeNull()
    expect(screen.queryByText('Aprovado por você')).toBeNull()
  })

  it('busca por chave e título, combinada com o filtro de tipo', async () => {
    mock(lean)
    wrap(<Board />, '/board')
    await screen.findByText('Card cru')
    const box = screen.getByRole('searchbox', { name: 'Buscar card' })
    fireEvent.change(box, { target: { value: 'revisao' } })
    expect(screen.getAllByRole('article').map((a) => a.getAttribute('aria-label'))).toEqual(['NAC-5 Em revisão velha', 'NAC-6 Em revisão nova'])
    fireEvent.change(box, { target: { value: 'nac-2' } })
    expect(screen.getAllByRole('article')).toHaveLength(4) // NAC-2 e NAC-21..25 cortados em 3
    fireEvent.change(box, { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Filtrar por tipo'), { target: { value: 'bug' } })
    expect(screen.getAllByRole('article').map((a) => a.getAttribute('aria-label'))).toEqual(['NAC-1 Card cru'])
    fireEvent.change(box, { target: { value: 'pausado' } })
    expect(screen.queryAllByRole('article')).toHaveLength(0)
  })
})
