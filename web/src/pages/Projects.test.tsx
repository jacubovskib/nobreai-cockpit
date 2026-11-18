import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Projects } from './Projects'
import { ProjectPage } from './ProjectPage'
import { range } from './analytics/data'

const urls: string[] = []
function mock(routes: Record<string, unknown>) {
  urls.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(url)
    const key = Object.keys(routes).find((k) => url === k || url.startsWith(k + '?'))
    return key === undefined ? new Response('{}', { status: 404 }) : new Response(JSON.stringify(routes[key]), { status: 200 })
  }))
}
function Where() { const l = useLocation(); return <p data-testid="loc">{l.pathname}{l.search}</p> }
function wrap(at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <Routes>
          <Route path="projetos" element={<Projects />} />
          <Route path="projetos/:id" element={<ProjectPage />} />
          <Route path="*" element={null} />
        </Routes>
        <Where />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}
afterEach(() => vi.unstubAllGlobals())

const ago = (h: number) => new Date(Date.now() - h * 3600000).toISOString()
const card = (o: object) => ({
  id: 1, key: 'NAC-1', title: 'x', column: 'backlog', project_id: 1, created_at: ago(500), updated_at: ago(500),
  last_activity_at: null, totals: { cost_usd: 0, unpriced: false }, signal: '', signal_since: null, ...o,
})
const routes = {
  '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'web', path: '/w/web', capture: false }],
  '/api/cards': [
    card({ id: 1, key: 'NAC-1', title: 'Feita', column: 'concluido', updated_at: ago(100) }),
    card({ id: 2, key: 'NAC-2', title: 'Rodando', column: 'em_andamento', last_activity_at: ago(1), updated_at: ago(2) }),
    card({ id: 3, key: 'NAC-3', title: 'Esquecida', column: 'pausado', signal: 'parado', signal_since: ago(50), updated_at: ago(50) }),
    card({ id: 4, key: 'NAC-4', title: 'Ideia', column: 'backlog' }),
    card({ id: 5, key: 'NAC-5', title: 'Para ver', column: 'revisao', signal: 'aguardando_revisao', signal_since: ago(73), updated_at: ago(73) }),
  ],
  '/api/usage/totals': [
    { key: '1', cost_usd: 12.5, unpriced: false },
    { key: '2', cost_usd: 0, unpriced: false },
  ],
}

describe('página Projetos', () => {
  it('mostra a tabela comparativa e pede os totais dos 7 dias', async () => {
    mock(routes)
    wrap('/projetos')
    expect(await screen.findByRole('columnheader', { name: 'Projeto' })).toBeInTheDocument()
    for (const h of ['Custo', 'Entrega atual', 'Parados', 'Aguardando você', 'Última atividade'])
      expect(screen.getByRole('columnheader', { name: h })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '7 dias' })).toHaveAttribute('aria-pressed', 'true')
    const { from } = range(7)
    await screen.findByText('Rodando')
    expect(urls.some((u) => u.includes('group=project') && u.includes(`from=${from}`))).toBe(true)
    expect(screen.getByText('captura desligada')).toBeInTheDocument()
    expect(screen.getByText('nada em andamento')).toBeInTheDocument()
    expect(screen.getByText('aguardando revisão há 3 dias')).toBeInTheDocument()
  })
  it('clicar no projeto leva ao cronograma', async () => {
    mock(routes)
    wrap('/projetos')
    fireEvent.click(await screen.findByRole('link', { name: /api/ }))
    expect(screen.getByTestId('loc')).toHaveTextContent('/projetos/1')
  })
})

describe('família em Projetos', () => {
  const fam = {
    ...routes,
    '/api/projects': [
      { id: 1, name: 'loja', path: '/w/loja', capture: true, parent_id: null },
      { id: 2, name: 'web', path: '/w/loja/web', capture: true, parent_id: 1 },
      { id: 3, name: 'api', path: '/w/loja/api', capture: true, parent_id: 1 },
    ],
    '/api/usage/totals': [{ key: '1', cost_usd: 26, unpriced: false }, { key: '2', cost_usd: 95.1, unpriced: false }, { key: '3', cost_usd: 61.05, unpriced: false }],
  }
  it('expands apps', async () => {
    mock(fam)
    wrap('/projetos')
    const btn = await screen.findByRole('button', { name: /apps/ })
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('geral')).toBeNull()
    fireEvent.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    for (const n of ['web', 'api', 'geral']) expect(screen.getByText(n)).toBeInTheDocument()
  })
  it('página do pai tem o quadro Por app, a do filho o link', async () => {
    mock(fam)
    const { unmount } = wrap('/projetos/1')
    expect(await screen.findByRole('heading', { name: 'Por app' })).toBeInTheDocument()
    expect(screen.getByText('total')).toBeInTheDocument()
    unmount()
    wrap('/projetos/2')
    expect(await screen.findByText('parte de')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'loja' })).toHaveAttribute('href', '/projetos/1')
  })
})

describe('página do projeto', () => {
  it('lista as entregas por grupo com sinal e link', async () => {
    mock(routes)
    wrap('/projetos/1')
    for (const g of ['Concluídas', 'Em andamento', 'Paradas', 'Backlog'])
      expect(await screen.findByRole('heading', { name: g })).toBeInTheDocument()
    const stopped = screen.getByRole('link', { name: /NAC-3/ })
    expect(stopped).toHaveTextContent('Esquecida')
    expect(stopped).toHaveTextContent('parado há 2 dias')
    expect(screen.getByRole('link', { name: /NAC-5/ })).toHaveTextContent('aguardando revisão há 3 dias')
    expect(within(screen.getByRole('link', { name: /NAC-1/ })).queryByText(/há/)).toBeNull()
    expect(screen.getByRole('link', { name: 'Ver no board' })).toHaveAttribute('href', '/board?projeto=1')
    fireEvent.click(stopped)
    expect(screen.getByTestId('loc')).toHaveTextContent('/cards/NAC-3')
  })
  it('projeto inexistente', async () => {
    mock(routes)
    wrap('/projetos/99')
    expect(await screen.findByText('Projeto não encontrado')).toBeInTheDocument()
  })
  it('falha em /api/projects mostra o erro, não "não encontrado"', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) =>
      url === '/api/projects' ? new Response('{}', { status: 500 }) : new Response(JSON.stringify((routes as Record<string, unknown>)[url] ?? {}), { status: 200 })))
    wrap('/projetos/1')
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar o projeto.')
    expect(screen.queryByText('Projeto não encontrado')).toBeNull()
  })
  it('falha em /api/cards mostra o erro', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) =>
      url === '/api/cards' ? new Response('{}', { status: 500 }) : new Response(JSON.stringify((routes as Record<string, unknown>)[url] ?? {}), { status: 200 })))
    wrap('/projetos/1')
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar o projeto.')
    expect(screen.queryByText('Nenhuma entrega')).toBeNull()
  })
})
