import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CardsList } from './CardsList'

function mock(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    return r === undefined ? new Response('{}', { status: 404 }) : new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap(at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[at]}><CardsList /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const card = (id: number, key: string, project_id: number | null) => ({
  id, key, title: `Título ${key}`, column: 'backlog', project_id, live: null, agent: '', totals: { cost_usd: 0, unpriced: false },
})
const routes = {
  '/api/projects': [{ id: 1, name: 'api', path: '/w/api', capture: true }, { id: 2, name: 'web', path: '/w/web', capture: true }],
  '/api/cards': [card(1, 'NAC-1', 1), card(2, 'NAC-2', 2), card(3, 'NAC-3', null)],
}

describe('CardsList · projeto', () => {
  it('tem a coluna Projeto com o nome e "Sem projeto"', async () => {
    mock(routes)
    wrap('/cards')
    expect(await screen.findByRole('columnheader', { name: 'Projeto' })).toBeInTheDocument()
    const row = (await screen.findByText('Título NAC-1')).closest('tr')!
    expect(within(row).getByText('api')).toBeInTheDocument()
    expect(within(screen.getByText('Título NAC-3').closest('tr')!).getByText('Sem projeto')).toBeInTheDocument()
  })
  it('o seletor global (?projeto=1) filtra os cards', async () => {
    mock(routes)
    wrap('/cards?projeto=1')
    expect(await screen.findByText('Título NAC-1')).toBeInTheDocument()
    expect(screen.queryByText('Título NAC-2')).toBeNull()
    expect(screen.queryByText('Título NAC-3')).toBeNull()
  })
})
