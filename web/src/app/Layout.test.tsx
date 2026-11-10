import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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

describe('Seletor de projeto', () => {
  const base = {
    '/api/health': { ok: true, version: '0.1.0' }, '/api/settings': { demo: false },
    '/api/projects': [{ id: 1, name: 'cockpit', path: '/w/cockpit', capture: true }, { id: 2, name: 'site', path: '/w/site', capture: true }],
    '/api/cards': [], '/api/usage/totals?group=project': [],
  }
  const hrefs = () => screen.getAllByRole('link').map((a) => a.getAttribute('href'))

  it('lista os projetos e leva o filtro aos links do menu', async () => {
    mockApi(base)
    renderApp('/board')
    const sel = await screen.findByRole('combobox', { name: 'Projeto' })
    await screen.findByRole('option', { name: 'site' })
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Todos os projetos', 'cockpit', 'site'])
    fireEvent.change(sel, { target: { value: '2' } })
    await waitFor(() => expect(hrefs().every((h) => h!.endsWith('?projeto=2'))).toBe(true))
    expect(screen.getByRole('link', { name: /Board/ })).toHaveAttribute('aria-current', 'page')
    fireEvent.change(sel, { target: { value: '' } })
    await waitFor(() => expect(hrefs().some((h) => h!.includes('?projeto='))).toBe(false))
    expect(sel).toHaveValue('')
  })
  it('id desconhecido na URL vale Todos', async () => {
    mockApi(base)
    renderApp('/?projeto=99')
    const sel = await screen.findByRole('combobox', { name: 'Projeto' })
    await screen.findByRole('option', { name: 'site' })
    expect(sel).toHaveValue('')
    expect(hrefs().some((h) => h!.includes('?projeto='))).toBe(false)
  })
})
