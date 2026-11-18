import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AddProject } from './AddProject'

const proj = (o: object) => ({ id: 1, name: 'x', path: '', capture: true, base_branch: '', parent_id: null, kind: 'repo', ...o })
const home = { path: '/h/w', parent: '/h', dirs: [{ name: 'bravonix', path: '/h/w/bravonix', git: false, registered: false }, { name: 'turing-back', path: '/h/w/turing-back', git: true, registered: true }, { name: 'docs', path: '/h/w/docs', git: false, registered: false }] }
const inside = { path: '/h/w/bravonix', parent: '/h/w', dirs: [{ name: 'a', path: '/h/w/bravonix/a', git: true, registered: false }, { name: 'poc', path: '/h/w/bravonix/poc', git: false, registered: false }] }
const sug = (rel: string, o: object = {}) => ({ path: `/h/w/bravonix/${rel}`, rel, name: rel, source: 'git', registered: null, blocked: '', ...o })
const scan = (o: object = {}) => ({
  path: '/h/w/bravonix', git: false, kind: 'group', name: 'bravonix', registered: null,
  suggestions: [sug('turing-back', { registered: proj({ id: 5, name: 'turing-back', path: '/h/w/bravonix/turing-back' }) }), sug('portal'), sug('docs-center'), sug('velho', { registered: proj({ id: 6, parent_id: 9 }), blocked: 'pertence a mscloud' })],
  ...o,
})

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const r = routes[`${init?.method ?? 'GET'} ${url}`]
    return r === undefined ? new Response('{"error":"x"}', { status: 404 }) : new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const bodies = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && i?.method === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
const open = (initialPath?: string) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><AddProject initialPath={initialPath} onClose={() => {}} /></QueryClientProvider>)
afterEach(() => vi.unstubAllGlobals())

describe('AddProject', () => {
  it('browses into a folder and shows git badges', async () => {
    mock({ 'GET /api/fs/dirs': home, 'GET /api/fs/dirs?path=%2Fh%2Fw%2Fbravonix': inside })
    open()
    const back = await screen.findByRole('button', { name: /turing-back/ })
    expect(within(back).getByText('git')).toBeInTheDocument()
    expect(within(back).getByText('cadastrado')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /bravonix/ }))
    const a = await screen.findByRole('button', { name: /^a\// })
    expect(within(a).getByText('git')).toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: 'Caminho da pasta' })).getByRole('button', { name: 'bravonix' })).toBeInTheDocument()
  })

  it('scan of a group folder lists suggestions checked and blocked disabled', async () => {
    mock({ 'GET /api/fs/dirs': home, 'GET /api/fs/scan?path=%2Fh%2Fw': scan({ path: '/h/w' }) })
    open()
    await screen.findByRole('button', { name: /bravonix/ })
    fireEvent.click(screen.getByRole('button', { name: 'Usar esta pasta' }))
    expect(await screen.findByText(/Pasta com repositórios \(grupo\)/)).toBeInTheDocument()
    expect(screen.getByLabelText(/portal/)).toBeChecked()
    expect(screen.getByLabelText(/turing-back/)).toBeChecked()
    expect(screen.getByText(/já cadastrado: entra no grupo/)).toBeInTheDocument()
    const velho = screen.getByLabelText(/velho/)
    expect(velho).toBeDisabled()
    expect(velho).not.toBeChecked()
    expect(screen.getByText(/pertence a mscloud/)).toBeInTheDocument()
  })

  it('register posts batch with checked children only', async () => {
    const fn = mock({
      'GET /api/fs/scan?path=%2Fh%2Fw%2Fbravonix': scan(),
      'POST /api/projects/batch': { project: proj({ id: 9, name: 'Bravo' }), children: [{}, {}], moved: { sessions: 1, cards: 3 } },
    })
    open('/h/w/bravonix')
    await screen.findByLabelText(/portal/)
    fireEvent.click(screen.getByLabelText(/docs-center/))
    fireEvent.change(screen.getByLabelText('Nome'), { target: { value: 'Bravo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cadastrar' }))
    expect(await screen.findByText('Bravo cadastrado com 2 sub-projetos.')).toBeInTheDocument()
    expect(screen.getByText('1 sessão e 3 cards passaram para os sub-projetos.')).toBeInTheDocument()
    expect(bodies(fn, 'POST', '/api/projects/batch')).toEqual([{
      path: '/h/w/bravonix', name: 'Bravo',
      children: [{ path: '/h/w/bravonix/turing-back' }, { path: '/h/w/bravonix/portal', name: 'portal' }],
    }])
    expect(bodies(fn, 'POST', '/api/projects/9/suggestions/dismiss')).toEqual([])
  })

  it('dismiss unchecked when box marked', async () => {
    const fn = mock({
      'GET /api/fs/scan?path=%2Fh%2Fw%2Fbravonix': scan(),
      'POST /api/projects/batch': { project: proj({ id: 9, name: 'bravonix' }), children: [{}], moved: { sessions: 0, cards: 0 } },
      'POST /api/projects/9/suggestions/dismiss': {},
    })
    open('/h/w/bravonix')
    await screen.findByLabelText(/portal/)
    fireEvent.click(screen.getByLabelText(/docs-center/))
    fireEvent.click(screen.getByLabelText('não sugerir as pastas desmarcadas'))
    fireEvent.click(screen.getByRole('button', { name: 'Cadastrar' }))
    expect(await screen.findByText('bravonix cadastrado com 1 sub-projeto.')).toBeInTheDocument()
    await waitFor(() => expect(bodies(fn, 'POST', '/api/projects/9/suggestions/dismiss')).toEqual([{ rels: ['docs-center'] }]))
  })

  it('manual add opens picker inside project folder', async () => {
    const fn = mock({
      'GET /api/fs/scan?path=%2Fh%2Fw%2Fbravonix': scan({ suggestions: [] }),
      'GET /api/fs/dirs?path=%2Fh%2Fw%2Fbravonix': inside,
      'GET /api/fs/dirs?path=%2Fh%2Fw%2Fbravonix%2Fa': { path: '/h/w/bravonix/a', parent: '/h/w/bravonix', dirs: [] },
      'POST /api/projects/batch': { project: proj({ id: 9 }), children: [{}], moved: { sessions: 0, cards: 0 } },
    })
    open('/h/w/bravonix')
    fireEvent.click(await screen.findByRole('button', { name: /Adicionar outra pasta como sub-projeto/ }))
    fireEvent.click(await screen.findByRole('button', { name: /^a\// }))
    await screen.findByText('Nenhuma subpasta.')
    fireEvent.click(await screen.findByRole('button', { name: 'Usar esta pasta' }))
    expect(fn.mock.calls.some(([u]) => u === '/api/fs/dirs?path=%2Fh%2Fw%2Fbravonix')).toBe(true)
    expect(await screen.findByLabelText(/a\/?\s*$/)).toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: 'Cadastrar' }))
    await waitFor(() => expect(bodies(fn, 'POST', '/api/projects/batch')[0].children).toEqual([{ path: '/h/w/bravonix/a', name: 'a' }]))
  })

  it('keeps the success text and notes it when dismissing fails', async () => {
    mock({
      'GET /api/fs/scan?path=%2Fh%2Fw%2Fbravonix': scan(),
      'POST /api/projects/batch': { project: proj({ id: 9, name: 'bravonix' }), children: [{}], moved: { sessions: 0, cards: 0 } },
    })
    open('/h/w/bravonix')
    await screen.findByLabelText(/portal/)
    fireEvent.click(screen.getByLabelText(/docs-center/))
    fireEvent.click(screen.getByLabelText('não sugerir as pastas desmarcadas'))
    fireEvent.click(screen.getByRole('button', { name: 'Cadastrar' }))
    expect(await screen.findByText('bravonix cadastrado com 1 sub-projeto.')).toBeInTheDocument()
    expect(screen.getByText('Cadastrado, mas não consegui recusar as pastas desmarcadas.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Cadastrar' })).not.toBeInTheDocument()
  })

  it('rescan of a registered group shows only new suggestions', async () => {
    const group = proj({ id: 9, name: 'bravonix', path: '/h/w/bravonix', kind: 'group' })
    mock({
      'GET /api/fs/scan?path=%2Fh%2Fw%2Fbravonix': scan({
        registered: group,
        suggestions: [sug('turing-back', { registered: proj({ id: 5, parent_id: 9 }) }), sug('novo')],
      }),
    })
    open('/h/w/bravonix')
    expect(await screen.findByLabelText(/novo/)).toBeChecked()
    expect(screen.queryByLabelText(/turing-back/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Nome')).toBeDisabled()
  })

  it('keeps the last folder when a pasted path fails', async () => {
    mock({ 'GET /api/fs/dirs': home })
    open()
    await screen.findByRole('button', { name: /bravonix/ })
    fireEvent.change(screen.getByLabelText('Colar caminho'), { target: { value: '/fora' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ir' }))
    expect(await screen.findByText(/erro|x/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /bravonix/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Usar esta pasta' })).toBeEnabled()
  })
})
