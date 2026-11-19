import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Memories } from './Memories'
import type { Memory } from '../api/types'

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{}', { status: 404 })
    if (r === 204) return new Response(null, { status: 204 })
    return new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const calls = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(at = '/?projeto=1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[at]}><Memories /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const projects = [
  { id: 1, name: 'loja', path: '/l', capture: true, base_branch: 'main', parent_id: null, kind: 'repo' },
  { id: 2, name: 'web', path: '/l/web', capture: true, base_branch: 'main', parent_id: 1, kind: 'repo' },
]
const mem = (o: Partial<Memory>): Memory => ({
  id: 1, project_id: 1, category: 'shell', rule: 'regra', do: 'faça', status: 'ativa', source: 'usuario', source_ref: '',
  position: 0, needs_review: false, created_at: '', updated_at: '', inherited: false, ...o,
})
const preview = { block: 'Memórias deste projeto\n- [shell] x → y', tokens: 120, chosen: 2, left: 1 }
const base = (list: Memory[], pid = 1) => ({
  '/api/projects': projects, [`/api/memories?project=${pid}`]: list, [`/api/memories/preview?project=${pid}`]: preview,
})

describe('Memórias: fila', () => {
  const list = [
    mem({ id: 1, status: 'candidata', source: 'napkin', source_ref: '/l/.claude/napkin.md', rule: 'use rtk', do: 'prefixe', needs_review: true }),
    mem({ id: 2, status: 'candidata', source: 'card', source_ref: 'NAC-12', rule: 'rode os testes', do: 'pnpm test' }),
  ]

  it('mostra origem e revisar, aprova e descarta', async () => {
    const fn = mock({ ...base(list), 'PATCH /api/memories/2': {} })
    wrap()
    const fila = await screen.findByRole('region', { name: 'Fila de candidatas' })
    expect(within(fila).getByText('napkin: /l/.claude/napkin.md')).toBeInTheDocument()
    expect(within(fila).getByText('card NAC-12')).toBeInTheDocument()
    expect(within(fila).getAllByText('revisar')).toHaveLength(1)
    const row2 = within(fila).getByText('rode os testes').closest('li')!
    fireEvent.click(within(row2).getByRole('button', { name: 'Aprovar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/2')).toHaveLength(1))
    expect(calls(fn, 'PATCH', '/api/memories/2')[0]).toMatchObject({ status: 'ativa', needs_review: false })
    fireEvent.click(within(row2).getByRole('button', { name: 'Descartar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/2')).toHaveLength(2))
    expect(calls(fn, 'PATCH', '/api/memories/2')[1]).toMatchObject({ status: 'descartada' })
  })

  it('aprova selecionadas em lote e mostra as recusadas', async () => {
    const fn = mock({ ...base(list), 'POST /api/memories/batch': { done: [2], refused: [{ id: 1, error: 'regra maior que 300 caracteres' }] } })
    wrap()
    const fila = await screen.findByRole('region', { name: 'Fila de candidatas' })
    fireEvent.click(within(fila).getByLabelText('Selecionar #1'))
    fireEvent.click(within(fila).getByLabelText('Selecionar #2'))
    fireEvent.click(within(fila).getByRole('button', { name: 'Aprovar selecionadas' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/memories/batch')).toHaveLength(1))
    expect(calls(fn, 'POST', '/api/memories/batch')[0]).toEqual({ ids: [1, 2], action: 'aprovar' })
    expect(await within(fila).findByText(/#1: regra maior que 300 caracteres/)).toBeInTheDocument()
  })

  it('desabilita Aprovar quando passa do limite e libera ao editar', async () => {
    const big = mem({ id: 3, status: 'candidata', source: 'napkin', rule: 'a'.repeat(312), do: 'ok', needs_review: true })
    const fn = mock({ ...base([big]), 'PATCH /api/memories/3': {} })
    wrap()
    const fila = await screen.findByRole('region', { name: 'Fila de candidatas' })
    expect(within(fila).getByText('312/300')).toBeInTheDocument()
    expect(within(fila).getByRole('button', { name: 'Aprovar' })).toBeDisabled()
    fireEvent.click(within(fila).getByRole('button', { name: 'Editar' }))
    fireEvent.change(within(fila).getByLabelText('Regra'), { target: { value: 'curta' } })
    fireEvent.change(within(fila).getByLabelText('Faça assim'), { target: { value: 'nova ação' } })
    fireEvent.click(within(fila).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/3')).toHaveLength(1))
    expect(calls(fn, 'PATCH', '/api/memories/3')[0]).toMatchObject({ rule: 'curta', do: 'nova ação', category: 'shell', project_id: 1 })
  })
})

describe('Memórias: ativas', () => {
  const list = [
    mem({ id: 10, project_id: 2, rule: 'rode lint', do: 'make lint', category: 'execucao' }),
    mem({ id: 11, project_id: 2, rule: 'Rode lint!', do: 'x', category: 'execucao', position: 1 }),
    mem({ id: 12, rule: 'use rtk', do: 'prefixe', category: 'shell', project_id: null, inherited: true }),
    mem({ id: 13, rule: 'sem acento aqui', do: 'a', category: 'dominio', project_id: 1, inherited: true }),
  ]

  it('agrupa por categoria, marca herdadas e duplicadas', async () => {
    mock(base(list, 2))
    wrap('/?projeto=1&app=2')
    const ativas = await screen.findByRole('region', { name: 'Memórias ativas' })
    expect(within(ativas).getByText('Execução e validação')).toBeInTheDocument()
    expect(within(ativas).getByText('Shell e comandos')).toBeInTheDocument()
    expect(within(ativas).getByText('Regras do domínio')).toBeInTheDocument()
    const g = within(ativas).getByText('use rtk').closest('li')!
    expect(within(g).getByText('global')).toBeInTheDocument()
    expect(within(g).queryByRole('button', { name: 'Subir' })).not.toBeInTheDocument()
    const r = within(ativas).getByText('sem acento aqui').closest('li')!
    expect(within(r).getByText('da raiz')).toBeInTheDocument()
    const dup = within(ativas).getByText('Rode lint!').closest('li')!
    expect(within(dup).getByText('Duplicada de #10')).toBeInTheDocument()
  })

  it('marca parecida', async () => {
    mock(base([mem({ id: 20, rule: 'rode lint antes do commit' }), mem({ id: 21, rule: 'rode o lint antes do commit sempre' })]))
    wrap()
    const ativas = await screen.findByRole('region', { name: 'Memórias ativas' })
    expect(within(ativas).getByText('Parecida com #20')).toBeInTheDocument()
  })

  it('sobe, arquiva e edita com escopo', async () => {
    const fn = mock({ ...base(list, 2), 'POST /api/memories/11/move': { ok: true }, 'PATCH /api/memories/11': {}, '/api/projects': projects })
    wrap('/?projeto=1&app=2')
    const ativas = await screen.findByRole('region', { name: 'Memórias ativas' })
    const row = within(ativas).getByText('Rode lint!').closest('li')!
    fireEvent.click(within(row).getByRole('button', { name: 'Subir' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/memories/11/move')).toEqual([{ dir: -1 }]))
    fireEvent.click(within(row).getByRole('button', { name: 'Arquivar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/11')).toHaveLength(1))
    expect(calls(fn, 'PATCH', '/api/memories/11')[0]).toEqual({ status: 'arquivada' })
    fireEvent.click(within(row).getByRole('button', { name: 'Editar' }))
    const scope = within(row).getByLabelText('Escopo') as HTMLSelectElement
    expect([...scope.options].map((o) => o.text)).toEqual(['este projeto', 'raiz', 'global'])
    fireEvent.change(scope, { target: { value: 'g' } })
    fireEvent.change(within(row).getByLabelText('Categoria'), { target: { value: 'diretiva' } })
    fireEvent.click(within(row).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/11')).toHaveLength(2))
    expect(calls(fn, 'PATCH', '/api/memories/11')[1]).toMatchObject({ project_id: null, category: 'diretiva' })
  })
})

describe('Memórias: prévia', () => {
  it('mostra o resumo e o bloco recolhível', async () => {
    mock(base([mem({}), mem({ id: 2, rule: 'outra' })]))
    wrap()
    const prev = await screen.findByRole('region', { name: 'Prévia da sessão' })
    expect(await within(prev).findByText('Entram 2 de 3 memórias (~120 tokens) na próxima sessão deste projeto')).toBeInTheDocument()
    expect(prev.querySelector('pre')?.textContent).toContain('[shell] x → y')
  })

  it('em Todos pede um projeto e lista todos os escopos', async () => {
    const fn = mock({ '/api/projects': projects, '/api/memories': [mem({ project_id: 2 }), mem({ id: 2, project_id: null, rule: 'g' })] })
    wrap('/')
    expect(await screen.findByText('Escolha um projeto para ver a prévia.')).toBeInTheDocument()
    expect(await screen.findByText('g')).toBeInTheDocument()
    expect(screen.queryByText('da raiz')).not.toBeInTheDocument()
    expect(fn.mock.calls.some(([u]) => String(u).includes('/preview'))).toBe(false)
  })
})

describe('Memórias: arquivadas, importação e criação', () => {
  it('restaura e apaga com confirmação em página', async () => {
    const fn = mock({ ...base([mem({ id: 5, status: 'arquivada', rule: 'velha' })]), 'PATCH /api/memories/5': {}, 'DELETE /api/memories/5': 204 })
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    wrap()
    const arq = await screen.findByRole('region', { name: 'Arquivadas' })
    fireEvent.click(within(arq).getByRole('button', { name: 'Restaurar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/5')[0]).toEqual({ status: 'ativa' }))
    fireEvent.click(within(arq).getByRole('button', { name: 'Apagar' }))
    expect(calls(fn, 'DELETE', '/api/memories/5')).toHaveLength(0)
    fireEvent.click(within(arq).getByRole('button', { name: 'Confirmar' }))
    await waitFor(() => expect(fn.mock.calls.some(([u, i]) => u === '/api/memories/5' && i?.method === 'DELETE')).toBe(true))
    expect(confirm).not.toHaveBeenCalled()
  })

  it('importa napkin.md', async () => {
    const fn = mock({ ...base([]), 'POST /api/memories/import': { created: 3, skipped: 2, files: [{ project_id: 1, path: '/l/.claude/napkin.md', items: 5 }] } })
    wrap()
    fireEvent.click(await screen.findByRole('button', { name: 'Importar napkin.md' }))
    expect(await screen.findByText('3 candidatas novas, 2 já existentes')).toBeInTheDocument()
    expect(screen.getByText('/l/.claude/napkin.md (5)')).toBeInTheDocument()
    expect(calls(fn, 'POST', '/api/memories/import')).toHaveLength(1)
  })

  it('cria memória manual', async () => {
    const fn = mock({ ...base([]), 'POST /api/memories': {} })
    wrap()
    const form = await screen.findByRole('region', { name: 'Nova memória' })
    fireEvent.change(within(form).getByLabelText('Regra'), { target: { value: 'nunca rode x' } })
    fireEvent.change(within(form).getByLabelText('Faça assim'), { target: { value: 'rode y' } })
    fireEvent.change(within(form).getByLabelText('Categoria'), { target: { value: 'diretiva' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Criar' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/memories')).toHaveLength(1))
    expect(calls(fn, 'POST', '/api/memories')[0]).toEqual({ project_id: 1, category: 'diretiva', rule: 'nunca rode x', do: 'rode y' })
  })
})

describe('Memórias: ajustes de revisão', () => {
  it('rascunho da nova memória sobrevive a refetch que muda a lista', async () => {
    let data = [mem({ id: 1 })]
    const fn = vi.fn(async (url: string) => {
      const body = url === '/api/projects' ? projects : url.includes('/preview') ? preview : url.startsWith('/api/memories') ? data : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    vi.stubGlobal('fetch', fn)
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={['/?projeto=1']}><Memories /></MemoryRouter></QueryClientProvider>)
    const form = await screen.findByRole('region', { name: 'Nova memória' })
    fireEvent.change(within(form).getByLabelText('Regra'), { target: { value: 'rascunho' } })
    data = [mem({ id: 1 }), mem({ id: 2, rule: 'outra regra' })]
    await qc.invalidateQueries({ queryKey: ['memories'] })
    await screen.findByText('outra regra')
    expect((within(screen.getByRole('region', { name: 'Nova memória' })).getByLabelText('Regra') as HTMLTextAreaElement).value).toBe('rascunho')
  })

  it('edita item herdado da raiz e move para o app', async () => {
    const fn = mock({ ...base([mem({ id: 7, project_id: 1, inherited: true, rule: 'da raiz' })], 2), 'PATCH /api/memories/7': {} })
    wrap('/?projeto=1&app=2')
    const ativas = await screen.findByRole('region', { name: 'Memórias ativas' })
    const row = within(ativas).getByText('da raiz', { selector: 'strong' }).closest('li')!
    fireEvent.click(within(row).getByRole('button', { name: 'Editar' }))
    const scope = within(row).getByLabelText('Escopo') as HTMLSelectElement
    expect([...scope.options].map((o) => `${o.text}:${o.value}`)).toEqual(['este projeto:2', 'raiz:1', 'global:g'])
    expect(scope.value).toBe('1')
    fireEvent.change(scope, { target: { value: '2' } })
    fireEvent.click(within(row).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PATCH', '/api/memories/7')[0]).toMatchObject({ project_id: 2 }))
  })

  it('candidata duplicada de uma ativa é marcada', async () => {
    mock(base([mem({ id: 1, rule: 'use rtk' }), mem({ id: 2, status: 'candidata', rule: 'Use RTK.', source: 'napkin' })]))
    wrap()
    const fila = await screen.findByRole('region', { name: 'Fila de candidatas' })
    expect(within(fila).getByText('Duplicada de #1')).toBeInTheDocument()
  })

  it('prévia com limite 0 e erro da prévia', async () => {
    mock({ ...base([]), '/api/settings': { memory_budget_tokens: 0 } })
    wrap()
    expect(await screen.findByText('Memórias desligadas (limite 0 na Configuração)')).toBeInTheDocument()
  })

  it('mostra erro quando a prévia falha', async () => {
    const r = base([]) as Record<string, unknown>
    delete r['/api/memories/preview?project=1']
    mock({ ...r, '/api/settings': { memory_budget_tokens: 1500 } })
    wrap()
    expect(await screen.findByRole('alert')).toBeInTheDocument()
  })

  it('o "revisar" some depois de salvar quando o servidor limpa needs_review', async () => {
    let data = [mem({ id: 3, status: 'candidata', needs_review: true, rule: 'a'.repeat(312) })]
    const fn = vi.fn(async (url: string, init?: { method?: string }) => {
      if (init?.method === 'PATCH') { data = [mem({ id: 3, status: 'candidata', needs_review: false, rule: 'curta' })]; return new Response('{}', { status: 200 }) }
      const body = url === '/api/projects' ? projects : url.includes('/preview') ? preview : url.startsWith('/api/memories') ? data : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    vi.stubGlobal('fetch', fn)
    wrap()
    const fila = await screen.findByRole('region', { name: 'Fila de candidatas' })
    expect(within(fila).getByText('revisar')).toBeInTheDocument()
    fireEvent.click(within(fila).getByRole('button', { name: 'Editar' }))
    fireEvent.change(within(fila).getByLabelText('Regra'), { target: { value: 'curta' } })
    fireEvent.click(within(fila).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(within(fila).queryByText('revisar')).not.toBeInTheDocument())
  })
})
