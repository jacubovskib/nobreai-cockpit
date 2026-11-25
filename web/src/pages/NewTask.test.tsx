import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { NewTask } from './NewTask'

const projects = [
  { id: 1, name: 'api', path: '/w/api', capture: true, kind: 'repo', parent_id: null },
  { id: 2, name: 'web', path: '/w/web', capture: true, kind: 'repo', parent_id: null },
  { id: 3, name: 'job', path: '/w/job', capture: true, kind: 'repo', parent_id: null },
]
const allProjects = [...projects, { id: 9, name: 'grupo', path: '', capture: false, kind: 'group', parent_id: null }]
const cardsList = [
  { key: 'NAC-7', title: 'Plataforma', type: 'epico', column: 'backlog' },
  { key: 'NAC-8', title: 'Velho', type: 'epico', column: 'concluido' },
  { key: 'NAC-9', title: 'Tarefa comum', type: 'tarefa', column: 'backlog' },
]
let fetchMock: ReturnType<typeof vi.fn>
let qcRef: QueryClient
function wrap(at: string, props: { epic?: string; label?: string } = {}) {
  fetchMock = vi.fn(async (url: string, init?: { method?: string }) =>
    url === '/api/projects' ? new Response(JSON.stringify(allProjects), { status: 200 })
      : url === '/api/cards' && (init?.method ?? 'GET') === 'GET' ? new Response(JSON.stringify(cardsList), { status: 200 })
      : url === '/api/cards' ? new Response('{}', { status: 201 })
      : new Response('{}', { status: 404 }))
  vi.stubGlobal('fetch', fetchMock)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  qcRef = qc
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <Link to="/?projeto=2">filtro web</Link>
        <Link to="/?projeto=3">filtro job</Link>
        <NewTask {...props} />
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

const sent = () => JSON.parse(fetchMock.mock.calls.find(([u, i]) => u === '/api/cards' && i?.method === 'POST')![1].body)

describe('NewTask · tipo e épico', () => {
  it('padrão envia type tarefa e nenhum parent_key', async () => {
    wrap('/?projeto=1')
    await select()
    await screen.findByRole('option', { name: 'web' })
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Algo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(sent()).toEqual({ title: 'Algo', goal: '', project_id: 1, type: 'tarefa' }))
  })
  it('escolher tipo e épico envia type e parent_key; épicos concluídos não aparecem', async () => {
    wrap('/?projeto=1')
    await select()
    await screen.findByRole('option', { name: 'NAC-7 · Plataforma' })
    expect(screen.queryByRole('option', { name: /NAC-8/ })).toBeNull()
    expect(screen.getByRole('option', { name: 'sem épico' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Algo' } })
    fireEvent.change(screen.getByLabelText('Tipo'), { target: { value: 'bug' } })
    fireEvent.change(screen.getByLabelText('Épico'), { target: { value: 'NAC-7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(sent()).toMatchObject({ type: 'bug', parent_key: 'NAC-7' }))
  })
  it('com type épico, grupos sem pasta aparecem e o seletor de épico some', async () => {
    wrap('/?projeto=1')
    await select()
    await screen.findByRole('option', { name: 'web' })
    expect(screen.queryByRole('option', { name: 'grupo' })).toBeNull()
    fireEvent.change(screen.getByLabelText('Tipo'), { target: { value: 'epico' } })
    expect(screen.getByRole('option', { name: 'grupo' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Épico')).toBeNull()
  })
  it('Nova filha abre com o épico preenchido e o projeto escolhível', async () => {
    wrap('/?projeto=1', { epic: 'NAC-7', label: 'Nova filha' })
    fireEvent.click(await screen.findByRole('button', { name: 'Nova filha' }))
    await screen.findByRole('option', { name: 'NAC-7 · Plataforma' })
    expect((screen.getByLabelText('Épico') as HTMLSelectElement).value).toBe('NAC-7')
    fireEvent.change(await screen.findByLabelText('Projeto'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Filha' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(sent()).toMatchObject({ project_id: 3, parent_key: 'NAC-7' }))
  })
  it('Nova filha invalida o card do épico para a lista de filhas atualizar', async () => {
    wrap('/?projeto=1', { epic: 'NAC-7', label: 'Nova filha' })
    const spy = vi.spyOn(qcRef, 'invalidateQueries')
    fireEvent.click(await screen.findByRole('button', { name: 'Nova filha' }))
    await screen.findByRole('option', { name: 'NAC-7 · Plataforma' })
    fireEvent.change(screen.getByLabelText('Título'), { target: { value: 'Filha' } })
    fireEvent.click(screen.getByRole('button', { name: 'Criar tarefa' }))
    await waitFor(() => expect(spy).toHaveBeenCalledWith({ queryKey: ['card', 'NAC-7'] }))
  })
})
