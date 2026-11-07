import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Terminals } from './Terminals'

vi.mock('../ui/TerminalView', () => ({
  TerminalView: ({ id, readonly }: { id: string; readonly?: boolean }) => <div data-testid="tv">{id}:{String(!!readonly)}</div>,
}))

const info = (o: object) => ({ id: 'a', card_key: 'NAC-1', agent: 'claude', origin: 'plataforma', title: '', pid: 1, started_at: new Date().toISOString(), ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 1, ...o })

function renderAt(path = '/terminals') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[path]}><Terminals /></MemoryRouter></QueryClientProvider>)
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
