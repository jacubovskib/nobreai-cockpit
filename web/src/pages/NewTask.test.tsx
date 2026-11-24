import { fireEvent, render, screen } from '@testing-library/react'
import { Link, MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { NewTask } from './NewTask'

const projects = [
  { id: 1, name: 'api', path: '/w/api', capture: true, kind: 'repo', parent_id: null },
  { id: 2, name: 'web', path: '/w/web', capture: true, kind: 'repo', parent_id: null },
  { id: 3, name: 'job', path: '/w/job', capture: true, kind: 'repo', parent_id: null },
]
function wrap(at: string) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) =>
    url === '/api/projects' ? new Response(JSON.stringify(projects), { status: 200 }) : new Response('{}', { status: 404 })))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <Link to="/?projeto=2">filtro web</Link>
        <Link to="/?projeto=3">filtro job</Link>
        <NewTask />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}
afterEach(() => vi.unstubAllGlobals())

const select = async () => {
  fireEvent.click(await screen.findByRole('button', { name: 'Nova tarefa' }))
  return (await screen.findByLabelText('Projeto')) as HTMLSelectElement
}

describe('NewTask · projeto', () => {
  it('abre com o projeto do seletor global pré-selecionado', async () => {
    wrap('/?projeto=2')
    const sel = await select()
    await screen.findByRole('option', { name: 'web' })
    expect(sel.value).toBe('2')
  })
  it('sem mexer no select, segue o seletor global', async () => {
    wrap('/?projeto=1')
    const sel = await select()
    await screen.findByRole('option', { name: 'web' })
    expect(sel.value).toBe('1')
    fireEvent.click(screen.getByText('filtro web'))
    expect(sel.value).toBe('2')
  })
  it('depois de uma escolha manual, a escolha fica', async () => {
    wrap('/?projeto=1')
    const sel = await select()
    await screen.findByRole('option', { name: 'web' })
    fireEvent.change(sel, { target: { value: '2' } })
    fireEvent.click(screen.getByText('filtro job'))
    expect(sel.value).toBe('2')
  })
})
