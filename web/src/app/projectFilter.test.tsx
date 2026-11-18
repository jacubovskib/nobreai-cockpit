import { renderHook, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useProjectFilter } from './project'

const projects = [
  { id: 1, name: 'loja', path: '/w/loja', capture: true, base_branch: '', parent_id: null, kind: 'group' },
  { id: 2, name: 'web', path: '/w/loja/web', capture: true, base_branch: '', parent_id: 1, kind: 'repo' },
  { id: 3, name: 'solo', path: '/w/solo', capture: true, base_branch: '', parent_id: null, kind: 'repo' },
]

function setup(search: string) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(projects), { status: 200 })))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}><MemoryRouter initialEntries={['/board' + search]}>{children}</MemoryRouter></QueryClientProvider>
  )
  return renderHook(() => useProjectFilter(), { wrapper })
}

afterEach(() => vi.unstubAllGlobals())

describe('useProjectFilter com app', () => {
  it('app numérico: projectId e project são o filho', async () => {
    const { result } = setup('?projeto=1&app=2')
    await waitFor(() => expect(result.current.root?.id).toBe(1))
    expect(result.current.projectId).toBe(2)
    expect(result.current.project?.id).toBe(2)
    expect(result.current.ids).toEqual([2])
  })
  it('geral: projectId e project são o principal', async () => {
    const { result } = setup('?projeto=1&app=geral')
    await waitFor(() => expect(result.current.root?.id).toBe(1))
    expect(result.current.app).toBe('geral')
    expect(result.current.projectId).toBe(1)
    expect(result.current.project?.id).toBe(1)
    expect(result.current.ids).toEqual([1])
  })
  it('Todos: principal e família', async () => {
    const { result } = setup('?projeto=1')
    await waitFor(() => expect(result.current.root?.id).toBe(1))
    expect(result.current.app).toBeNull()
    expect(result.current.projectId).toBe(1)
    expect(result.current.ids).toEqual([1, 2])
  })
  it('app que não é filho é ignorado', async () => {
    const { result } = setup('?projeto=1&app=3')
    await waitFor(() => expect(result.current.root?.id).toBe(1))
    expect(result.current.app).toBeNull()
    expect(result.current.projectId).toBe(1)
    expect(result.current.ids).toEqual([1, 2])
  })
  it('app em projeto sem filhos é ignorado', async () => {
    const { result } = setup('?projeto=3&app=geral')
    await waitFor(() => expect(result.current.root?.id).toBe(3))
    expect(result.current.app).toBeNull()
    expect(result.current.ids).toEqual([3])
  })
})
