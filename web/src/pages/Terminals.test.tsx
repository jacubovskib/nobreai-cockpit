import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Terminals } from './Terminals'

vi.mock('../ui/TerminalView', () => ({
  TerminalView: ({ id, readonly }: { id: string; readonly?: boolean }) => <div data-testid="tv">{id}:{String(!!readonly)}</div>,
}))

const info = (o: object) => ({ id: 'a', card_key: 'NAC-1', agent: 'claude', origin: 'plataforma', title: '', pid: 1, started_at: new Date().toISOString(), ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 1, ...o })

function Where() {
  return <output data-testid="where">{useLocation().search}</output>
}

function renderAt(path = '/terminals') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[path]}><Terminals /><Where /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

describe('Terminals', () => {
  it('lista os terminais e abre o selecionado', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      info({ id: 'a', card_key: 'NAC-1' }),
      info({ id: 'b', card_key: '', ended_at: new Date().toISOString(), exit_code: 2, started_at: new Date(Date.now() - 60000).toISOString() }),
    ]), { status: 200 })))
    renderAt()
    expect(await screen.findByRole('link', { name: 'NAC-1' })).toBeInTheDocument()
    expect(screen.getByText('ao vivo')).toBeInTheDocument()
    expect(screen.getByText('encerrado (código 2)')).toBeInTheDocument()
    expect(screen.queryByTestId('tv')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Abrir terminal Claude Code do card NAC-1/ }))
    expect(await screen.findByTestId('tv')).toHaveTextContent('a:false')
  })

  it('terminal do nobre run abre em somente assistir', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([info({ id: 'r', origin: 'nobre run' })]), { status: 200 })))
    renderAt('/terminals?id=r')
    expect(await screen.findByTestId('tv')).toHaveTextContent('r:true')
  })

  it('com ?projeto=1 mantém terminal de outro projeto, com a etiqueta achada pelo diretório', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => new Response(JSON.stringify(
      url === '/api/projects'
        ? [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'site', path: '/w/site', capture: true }]
        : [info({ id: 'a', card_key: 'NAC-1', dir: '/w/site/sub' }), info({ id: 'b', card_key: 'NAC-2', dir: '/outro' })],
    ), { status: 200 })))
    renderAt('/terminals?projeto=1')
    expect(await screen.findByRole('link', { name: 'NAC-1' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'NAC-2' })).toBeInTheDocument()
    expect(await screen.findByText('site')).toBeInTheDocument()
    expect(screen.getByText('Sem projeto')).toBeInTheDocument()
  })

  it('selecionar um terminal mantém o projeto na URL', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([info({ id: 'a', card_key: 'NAC-1' })]), { status: 200 })))
    renderAt('/terminals?projeto=2')
    fireEvent.click(await screen.findByRole('button', { name: /Abrir terminal/ }))
    expect(screen.getByTestId('where')).toHaveTextContent('projeto=2')
    expect(screen.getByTestId('where')).toHaveTextContent('id=a')
  })

  it('terminal com card usa o projeto do card, não o diretório', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => new Response(JSON.stringify(
      url === '/api/projects'
        ? [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'site', path: '/w/site', capture: true }]
        : url === '/api/cards'
          ? [{ id: 5, key: 'NAC-1', title: 'x', project_id: 2, column: 'backlog', totals: { cost_usd: 0, unpriced: false }, readiness: { met: 0, total: 5, ready: false }, signal: '', signal_since: null }]
          : [info({ id: 'a', card_key: 'NAC-1', dir: '/fora/de/tudo' })],
    ), { status: 200 })))
    renderAt()
    expect(await screen.findByText('site')).toBeInTheDocument()
    expect(screen.queryByText('Sem projeto')).toBeNull()
  })

  it('mostra estado vazio', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { status: 200 })))
    renderAt()
    expect(await screen.findByText(/Nenhum terminal aberto/)).toBeInTheDocument()
  })

  it('id desconhecido mostra que o terminal não existe mais', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { status: 200 })))
    renderAt('/terminals?id=sumiu')
    expect(await screen.findByText('Este terminal não existe mais (encerrado e removido).')).toBeInTheDocument()
  })
})
