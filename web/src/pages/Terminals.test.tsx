import '@testing-library/jest-dom/vitest'
import { forwardRef, useImperativeHandle } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { TerminalHandle, TerminalViewProps } from '../ui/TerminalView'
import { KEY } from './terminals/gridStore'
import { Terminals } from './Terminals'

// Terminal simulado: conta quantas vezes cada tile desenhou o terminal (o
// GridTile real redesenha o filho a cada render dele).
const renders: Record<string, number> = {}
vi.mock('../ui/LazyTerminalView', () => ({
  LazyTerminalView: forwardRef<TerminalHandle, TerminalViewProps>(function Fake(props, ref) {
    renders[props.id] = (renders[props.id] ?? 0) + 1
    useImperativeHandle(ref, () => ({ setReadonly() {}, kill() {}, fit() {}, reconnect() {} }))
    return <div data-testid="tv" data-id={props.id} data-readonly={String(!!props.readonly)} data-active={String(!!props.active)}>{props.id}</div>
  }),
}))

const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()
const info = (o: object) => ({ id: 'a', card_key: 'NAC-1', agent: 'claude', origin: 'plataforma', title: '', pid: 1, started_at: new Date().toISOString(), ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 1, ...o })
const proj = (id: number, name: string, path: string, parent_id: number | null = null, kind = 'repo') => ({ id, name, path, capture: true, base_branch: 'main', parent_id, kind })

// loja tem filhos (web, adm); solo não tem.
const PROJECTS = [proj(1, 'loja', '/w/loja', null, 'group'), proj(2, 'web', '/w/loja/web', 1), proj(3, 'adm', '/w/loja/adm', 1), proj(4, 'solo', '/w/solo')]
const FAMILY = [
  info({ id: 't1', card_key: 'NAC-1', dir: '/w/loja', started_at: ago(1) }),
  info({ id: 't2', card_key: 'NAC-2', dir: '/w/loja/web', started_at: ago(2) }),
  info({ id: 't3', card_key: 'NAC-3', dir: '/w/solo', started_at: ago(3) }),
  info({ id: 't4', card_key: 'NAC-4', dir: '/outro', started_at: ago(4) }),
]

function api(terms: object[], projects: object[] = [], cards: object[] = []) {
  const fetch = vi.fn(async (url: string) => new Response(JSON.stringify(
    url === '/api/projects' ? projects : url === '/api/cards' ? cards : terms,
  ), { status: 200 }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

function Where() {
  return <output data-testid="where">{useLocation().search}</output>
}

function renderAt(path = '/terminals') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { qc, ...render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[path]}><Terminals /><Where /></MemoryRouter></QueryClientProvider>) }
}

const tiles = () => Array.from(document.querySelectorAll<HTMLElement>('section[data-tile]'))
const tileIds = () => tiles().map((s) => s.getAttribute('data-tile'))
const tile = (id: string) => document.querySelector<HTMLElement>(`section[data-tile="${id}"]`)!
const saved = () => JSON.parse(localStorage.getItem(KEY) ?? 'null')
const tabList = () => screen.getByRole('tablist', { name: 'Projetos' })
const findTabList = () => screen.findByRole('tablist', { name: 'Projetos' })
// Deixa as consultas e os efeitos assentarem antes de medir.
const settle = async () => { for (let i = 0; i < 5; i++) await act(() => new Promise((r) => setTimeout(r, 0))) }

beforeEach(() => {
  localStorage.clear()
  for (const k of Object.keys(renders)) delete renders[k]
})
afterEach(() => vi.unstubAllGlobals())

describe('Terminais em grade', () => {
  it('abas com contagem; a aba filtra; chips só em projeto com filhos; Geral lista os da raiz', async () => {
    api(FAMILY, PROJECTS)
    renderAt()
    expect(await within(await findTabList()).findByRole('tab', { name: 'loja 2' })).toBeInTheDocument()
    expect(within(tabList()).getByRole('tab', { name: 'Todos 4' })).toHaveAttribute('aria-selected', 'true')
    expect(within(tabList()).getByRole('tab', { name: 'solo 1' })).toBeInTheDocument()
    expect(within(tabList()).getAllByRole('tab')).toHaveLength(3)
    expect(tileIds()).toEqual(['t1', 't2', 't3', 't4'])
    expect(screen.queryByRole('group', { name: 'Subprojetos' })).toBeNull()

    fireEvent.click(within(tabList()).getByRole('tab', { name: 'loja 2' }))
    expect(tileIds()).toEqual(['t1', 't2'])
    const subs = screen.getByRole('group', { name: 'Subprojetos' })
    expect(within(subs).getAllByRole('button').map((b) => b.textContent)).toEqual(['Todos 2', 'Geral 1', 'web 1'])
    fireEvent.click(within(subs).getByRole('button', { name: 'Geral 1' }))
    expect(tileIds()).toEqual(['t1'])
    fireEvent.click(within(subs).getByRole('button', { name: 'web 1' }))
    expect(tileIds()).toEqual(['t2'])

    fireEvent.click(within(tabList()).getByRole('tab', { name: 'solo 1' }))
    expect(tileIds()).toEqual(['t3'])
    expect(screen.queryByRole('group', { name: 'Subprojetos' })).toBeNull()
    // Aba é estado da tela: não grava ?projeto= na URL.
    expect(screen.getByTestId('where')).not.toHaveTextContent('projeto')
    expect(saved().tab).toBe(4)
  })

  it('Simples/Detalhado e colunas regravam o layout salvo', async () => {
    api([info({ id: 'a' })])
    renderAt()
    await screen.findByText('plataforma')
    const modo = screen.getByRole('group', { name: 'Modo' })
    fireEvent.click(within(modo).getByRole('button', { name: 'Simples' }))
    expect(saved().simple).toBe(true)
    expect(screen.queryByText('plataforma')).toBeNull()
    fireEvent.click(within(modo).getByRole('button', { name: 'Detalhado' }))
    expect(saved().simple).toBe(false)
    expect(screen.getByText('plataforma')).toBeInTheDocument()

    const cols = screen.getByRole('group', { name: 'Colunas' })
    fireEvent.click(within(cols).getByRole('button', { name: '1 coluna' }))
    expect(saved().cols).toBe(1)
    expect(within(cols).getByRole('button', { name: '1 coluna' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('grade').style.gridTemplateColumns).toBe('repeat(1, minmax(0px, 1fr))')
  })

  it('filtro Ao vivo esconde os encerrados', async () => {
    api([info({ id: 'a' }), info({ id: 'b', card_key: 'NAC-2', ended_at: new Date().toISOString(), exit_code: 2, started_at: ago(1) })])
    renderAt()
    expect(await screen.findByText('encerrado (código 2)')).toBeInTheDocument()
    expect(tileIds()).toEqual(['a', 'b'])
    fireEvent.click(within(screen.getByRole('group', { name: 'Estado' })).getByRole('button', { name: 'Ao vivo' }))
    expect(tileIds()).toEqual(['a'])
    fireEvent.click(within(screen.getByRole('group', { name: 'Estado' })).getByRole('button', { name: 'Todos' }))
    expect(tileIds()).toEqual(['a', 'b'])
  })

  it('Restaurar layout volta ordem e tamanhos ao padrão', async () => {
    localStorage.setItem(KEY, JSON.stringify({ order: ['b', 'a'], sizes: { a: { span: 2, h: 500 } }, cols: 3, simple: false, tab: 'all', sub: 'all' }))
    api([info({ id: 'a', started_at: ago(1) }), info({ id: 'b', card_key: 'NAC-2', started_at: ago(2) })])
    renderAt()
    await screen.findAllByTestId('tv')
    expect(tileIds()).toEqual(['b', 'a'])
    expect(tile('a').style.gridColumn).toBe('span 2')
    fireEvent.click(screen.getByRole('button', { name: 'Restaurar layout' }))
    expect(tileIds()).toEqual(['a', 'b'])
    expect(tile('a').style.gridColumn).toBe('span 1')
    expect(tile('a').style.height).toBe('360px')
    expect(saved().order).toEqual(['a', 'b'])
    expect(saved().sizes).toEqual({})
  })

  it('arrastar A para depois de B muda a ordem na tela e no layout salvo', async () => {
    api([info({ id: 'a', started_at: ago(1) }), info({ id: 'b', card_key: 'NAC-2', started_at: ago(2) }), info({ id: 'c', card_key: 'NAC-3', started_at: ago(3) })])
    renderAt()
    await screen.findAllByTestId('tv')
    expect(tileIds()).toEqual(['a', 'b', 'c'])
    const b = tile('b')
    vi.spyOn(b, 'getBoundingClientRect').mockReturnValue({ left: 0, width: 400, top: 0, height: 360, right: 400, bottom: 360, x: 0, y: 0, toJSON() {} })
    const at = (type: string, x: number) => {
      const e = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperty(e, 'clientX', { value: x })
      fireEvent(b, e)
    }
    fireEvent.dragStart(within(tile('a')).getByTestId('tile-header'))
    at('dragover', 300)
    expect(tile('b').style.boxShadow).toContain('inset -4px')
    at('drop', 300)
    fireEvent.dragEnd(within(tile('a')).getByTestId('tile-header'))
    expect(tileIds()).toEqual(['b', 'a', 'c'])
    expect(saved().order).toEqual(['b', 'a', 'c'])
    expect(tile('b').style.boxShadow).toBe('')
  })

  it('?id=X abre na aba Todos com X em foco e ocupando a linha toda, mesmo com aba salva e filtro global', async () => {
    localStorage.setItem(KEY, JSON.stringify({ order: [], sizes: {}, cols: 3, simple: false, tab: 4, sub: 'all' }))
    api(FAMILY, PROJECTS)
    renderAt('/terminals?id=t1&projeto=4')
    await waitFor(() => expect(tile('t1').style.gridColumn).toBe('span 3'))
    await within(tabList()).findByRole('tab', { name: 'loja 2' })
    expect(within(tabList()).getByRole('tab', { name: 'Todos 4' })).toHaveAttribute('aria-selected', 'true')
    expect(tileIds()).toEqual(['t1', 't2', 't3', 't4'])
    expect(within(tile('t1')).getByTestId('tv')).toHaveAttribute('data-active', 'true')
    expect(within(tile('t2')).getByTestId('tv')).toHaveAttribute('data-active', 'false')
  })

  it('?id=X rola até o tile já ampliado', async () => {
    const seen: string[] = []
    const spy = vi.fn(function (this: HTMLElement) { seen.push(`${this.getAttribute('data-tile')}:${this.style.gridColumn}`) })
    Object.defineProperty(Element.prototype, 'scrollIntoView', { value: spy, configurable: true, writable: true })
    try {
      api(FAMILY, PROJECTS)
      renderAt('/terminals?id=t3')
      await waitFor(() => expect(spy).toHaveBeenCalledOnce())
      expect(seen).toEqual(['t3:span 3'])
    } finally {
      delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView
    }
  })

  it('?id=X que chega numa recarga da lista ainda abre em foco e na linha toda', async () => {
    let terms: object[] = [info({ id: 'a' })]
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(terms), { status: 200 })))
    const { qc } = renderAt('/terminals?id=novo')
    await screen.findByTestId('tv')
    terms = [info({ id: 'novo', card_key: 'NAC-9' }), info({ id: 'a', started_at: ago(1) })]
    await act(() => qc.invalidateQueries({ queryKey: ['terminals'] }))
    await waitFor(() => expect(tile('novo').style.gridColumn).toBe('span 3'))
    expect(within(tile('novo')).getByTestId('tv')).toHaveAttribute('data-active', 'true')
  })

  it('?id=inexistente avisa que o terminal não existe mais e a grade continua', async () => {
    api([info({ id: 'a' })])
    renderAt('/terminals?id=sumiu')
    expect(await screen.findByText('Este terminal não existe mais (encerrado e removido).')).toBeInTheDocument()
    expect(tileIds()).toEqual(['a'])
  })

  it('com o filtro global de projeto e sem id, abre na aba do projeto', async () => {
    api(FAMILY, PROJECTS)
    renderAt('/terminals?projeto=4')
    await waitFor(() => expect(within(tabList()).getByRole('tab', { name: 'solo 1' })).toHaveAttribute('aria-selected', 'true'))
    expect(tileIds()).toEqual(['t3'])
  })

  it('filtro global num subprojeto abre na aba do pai com o chip dele', async () => {
    api(FAMILY, PROJECTS)
    renderAt('/terminals?projeto=2')
    await waitFor(() => expect(within(tabList()).getByRole('tab', { name: 'loja 2' })).toHaveAttribute('aria-selected', 'true'))
    expect(within(screen.getByRole('group', { name: 'Subprojetos' })).getByRole('button', { name: 'web 1' })).toHaveAttribute('aria-pressed', 'true')
    expect(tileIds()).toEqual(['t2'])
  })

  it('filtro global num projeto sem terminais mostra Todos, com a etiqueta achada pelo diretório', async () => {
    api([info({ id: 'a', card_key: 'NAC-1', dir: '/w/site/sub' }), info({ id: 'b', card_key: 'NAC-2', dir: '/outro' })],
      [proj(1, 'api', '/w/api'), proj(2, 'site', '/w/site')])
    renderAt('/terminals?projeto=1')
    expect(await within(await findTabList()).findByRole('tab', { name: 'site 1' })).toBeInTheDocument()
    expect(within(tabList()).getByRole('tab', { name: 'Todos 2' })).toHaveAttribute('aria-selected', 'true')
    expect(within(tile('a')).getByText('site')).toBeInTheDocument()
    expect(within(tile('b')).getByText('Sem projeto')).toBeInTheDocument()
  })

  it('terminal com card usa o projeto do card, não o diretório', async () => {
    api([info({ id: 'a', card_key: 'NAC-1', dir: '/fora/de/tudo' })],
      [proj(1, 'api', '/w/api'), proj(2, 'site', '/w/site')],
      [{ id: 5, key: 'NAC-1', title: 'x', project_id: 2, column: 'backlog', totals: { cost_usd: 0, unpriced: false }, readiness: { met: 0, total: 5, ready: false }, signal: '', signal_since: null }])
    renderAt()
    expect(await within(await findTabList()).findByRole('tab', { name: 'site 1' })).toBeInTheDocument()
    expect(within(tile('a')).getByText('site')).toBeInTheDocument()
    expect(screen.queryByText('Sem projeto')).toBeNull()
  })

  it('terminal de nobre run e terminal da plataforma abrem ambos só assistindo', async () => {
    api([info({ id: 'r', origin: 'nobre run' }), info({ id: 'p', card_key: 'NAC-2', origin: 'plataforma' })])
    renderAt()
    const tvs = await screen.findAllByTestId('tv')
    expect(tvs).toHaveLength(2)
    for (const tv of tvs) expect(tv).toHaveAttribute('data-readonly', 'true')
  })

  it('com 12 terminais vivos, 9 tiles ligados e 3 em pausa', async () => {
    api(Array.from({ length: 12 }, (_, i) => info({ id: `t${i}`, card_key: `NAC-${i}`, started_at: ago(i) })))
    renderAt()
    await waitFor(() => expect(screen.getAllByTestId('tv')).toHaveLength(9))
    expect(screen.getAllByRole('button', { name: 'Em pausa. Clique para ligar.' })).toHaveLength(3)
    expect(tiles()).toHaveLength(12)
  })

  it('clicar num tile em pausa liga ele', async () => {
    api(Array.from({ length: 10 }, (_, i) => info({ id: `t${i}`, card_key: `NAC-${i}`, started_at: ago(i) })))
    renderAt()
    await waitFor(() => expect(screen.getAllByTestId('tv')).toHaveLength(9))
    fireEvent.click(within(tile('t9')).getByRole('button', { name: 'Em pausa. Clique para ligar.' }))
    expect(within(tile('t9')).getByTestId('tv')).toBeInTheDocument()
    expect(screen.getAllByTestId('tv')).toHaveLength(9)
  })

  it('interagir num tile não redesenha o outro', async () => {
    api([info({ id: 'a', started_at: ago(1) }), info({ id: 'b', card_key: 'NAC-2', started_at: ago(2) })])
    renderAt()
    await waitFor(() => expect(screen.getAllByTestId('tv')).toHaveLength(2))
    await settle()
    // O primeiro clique leva o foco para A; o segundo é a interação medida.
    fireEvent.click(within(tile('a')).getByRole('button', { name: 'Aumentar terminal' }))
    await settle()
    const b = renders.b, a = renders.a
    fireEvent.click(within(tile('a')).getByRole('button', { name: 'Aumentar terminal' }))
    await settle()
    expect(tile('a').style.gridColumn).toBe('span 3')
    expect(renders.a).toBeGreaterThan(a)
    expect(renders.b).toBe(b)
  })

  it('mostra carregando', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    renderAt()
    expect(screen.getByText('Carregando…')).toBeInTheDocument()
  })

  it('mostra erro ao carregar', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 500 })))
    renderAt()
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar os terminais.')
  })

  it('mostra estado vazio', async () => {
    api([])
    renderAt()
    expect(await screen.findByText('Nenhum terminal aberto. Inicie um agente em um card, em segundo plano, para vê-lo aqui.')).toBeInTheDocument()
  })

  it('id desconhecido mostra que o terminal não existe mais', async () => {
    api([])
    renderAt('/terminals?id=sumiu')
    expect(await screen.findByText('Este terminal não existe mais (encerrado e removido).')).toBeInTheDocument()
  })
})
