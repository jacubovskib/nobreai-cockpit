import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppRoutes } from './routes'

function mockApi(routes: Record<string, unknown | Error>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    if (r === undefined || r instanceof Error) throw r ?? new Error('sem rota ' + url)
    return new Response(JSON.stringify(r), { status: 200 })
  }))
}

function renderApp(path = '/board') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}><AppRoutes /></MemoryRouter>
    </QueryClientProvider>,
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('Layout', () => {
  it('mostra a faixa de demonstração e o daemon ativo', async () => {
    mockApi({ '/api/health': { ok: true, version: '0.1.0' }, '/api/settings': { demo: true } })
    renderApp()
    expect(await screen.findByText('Dados de demonstração')).toBeInTheDocument()
    expect(await screen.findByText('nobred ativo')).toBeInTheDocument()
    expect(screen.getByText('127.0.0.1:7420 · v0.1.0')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Board/ })).toHaveAttribute('aria-current', 'page')
  })
  it('sem demo não mostra a faixa; daemon fora do ar fica vermelho', async () => {
    mockApi({ '/api/health': new Error('x'), '/api/settings': { demo: false } })
    renderApp('/')
    expect(await screen.findByText('nobred fora do ar')).toBeInTheDocument()
    expect(screen.queryByText('Dados de demonstração')).not.toBeInTheDocument()
  })
})

describe('Navegação', () => {
  it('tem a página de worktrees no menu', async () => {
    mockApi({ '/api/health': { ok: true, version: '0.1.0' }, '/api/settings': { demo: false }, '/api/worktrees': [] })
    renderApp('/worktrees')
    expect(await screen.findByRole('heading', { name: 'Worktrees' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Worktrees/ })).toHaveAttribute('aria-current', 'page')
  })
})
