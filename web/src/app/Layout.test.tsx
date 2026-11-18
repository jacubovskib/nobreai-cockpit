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
  it('id não numérico na URL vale Todos e os links ficam sem projeto', async () => {
    mockApi(base)
    renderApp('/?projeto=abc')
    const sel = await screen.findByRole('combobox', { name: 'Projeto' })
    await screen.findByRole('option', { name: 'site' })
    expect(sel).toHaveValue('')
    expect(screen.getByRole('option', { name: 'Todos os projetos' })).toBeInTheDocument()
    expect(hrefs().some((h) => h!.includes('?projeto='))).toBe(false)
  })

  describe('dois passos', () => {
    const fam = {
      ...base,
      '/api/projects': [
        { id: 1, name: 'bravonix', path: '/w/b', capture: true, parent_id: null, kind: 'group' },
        { id: 2, name: 'web', path: '/w/b/web', capture: true, parent_id: 1, kind: 'repo' },
        { id: 3, name: 'api', path: '/w/b/api', capture: true, parent_id: 1, kind: 'repo' },
        { id: 4, name: 'site', path: '/w/site', capture: true, parent_id: null, kind: 'repo' },
      ],
    }
    it('second select appears only for a project with children', async () => {
      mockApi(fam)
      renderApp('/board')
      const sel = await screen.findByRole('combobox', { name: 'Projeto' })
      await screen.findByRole('option', { name: 'site' })
      expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Todos os projetos', 'bravonix', 'site'])
      expect(screen.queryByRole('combobox', { name: 'App' })).not.toBeInTheDocument()
      fireEvent.change(sel, { target: { value: '1' } })
      const app = await screen.findByRole('combobox', { name: 'App' })
      expect(Array.from(app.querySelectorAll('option')).map((o) => o.textContent)).toEqual(['Todos', 'geral (raiz)', 'api', 'web'])
      fireEvent.change(sel, { target: { value: '4' } })
      await waitFor(() => expect(screen.queryByRole('combobox', { name: 'App' })).not.toBeInTheDocument())
    })
    it('choosing an app sets &app= and changing project clears it', async () => {
      mockApi(fam)
      renderApp('/board')
      const sel = await screen.findByRole('combobox', { name: 'Projeto' })
      await screen.findByRole('option', { name: 'bravonix' })
      fireEvent.change(sel, { target: { value: '1' } })
      const app = await screen.findByRole('combobox', { name: 'App' })
      fireEvent.change(app, { target: { value: '2' } })
      await waitFor(() => expect(hrefs().every((h) => h!.endsWith('?projeto=1&app=2'))).toBe(true))
      fireEvent.change(app, { target: { value: 'geral' } })
      await waitFor(() => expect(hrefs().every((h) => h!.endsWith('?projeto=1&app=geral'))).toBe(true))
      fireEvent.change(sel, { target: { value: '' } })
      await waitFor(() => expect(hrefs().some((h) => h!.includes('app='))).toBe(false))
      fireEvent.change(sel, { target: { value: '1' } })
      expect(await screen.findByRole('combobox', { name: 'App' })).toHaveValue('')
    })
    it('legacy ?projeto=<child> is shown as parent + app', async () => {
      mockApi(fam)
      renderApp('/board?projeto=3')
      const sel = await screen.findByRole('combobox', { name: 'Projeto' })
      const app = await screen.findByRole('combobox', { name: 'App' })
      expect(sel).toHaveValue('1')
      expect(app).toHaveValue('3')
      expect(hrefs().every((h) => h!.endsWith('?projeto=1&app=3'))).toBe(true)
    })
  })
})
