import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ContextSection } from './ContextSection'

const urls: string[] = []
function mock(routes: Record<string, unknown>) {
  urls.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(url)
    const key = Object.keys(routes).find((k) => url === k || url.startsWith(k + '?'))
    return key === undefined ? new Response('{}', { status: 404 }) : new Response(JSON.stringify(routes[key]), { status: 200 })
  }))
}
function show(projectId: number | null = null, exact = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><ContextSection from="2026-11-11" to="2026-11-18" projectId={projectId} exact={exact} /></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const top = {
  keys: [
    { tool: 'Read', key: 'Read README.md', count: 3, tokens: 12000, in_tokens: 500, avg_tokens: 4166, weight: 90000 },
    { tool: 'hook', key: 'SessionStart · cockpit', count: 10, tokens: 2000, in_tokens: 0, avg_tokens: 200, weight: 5000 },
  ],
  sessions: [
    { session_id: 's1', card_key: 'NAC-7', card_title: 'Medir', started_at: '2026-11-15T12:00:00Z', peak_context: 150000, peak_pct: 0, compactions: 2, weight: 80000 },
    { session_id: 's2', card_key: '', card_title: '', started_at: '2026-11-16T12:00:00Z', peak_context: 50000, peak_pct: 0, compactions: 0, weight: 1000 },
  ],
  projects: [{ project_id: 1, weight: 95000, top_tool: 'Read', top_key: 'Read README.md' }],
  context_total: 200000,
  weight_total: 95000,
}
const detail = {
  turns: [
    { turn: 1, at: 'a', tokens: 1000, after_compact: false },
    { turn: 2, at: 'b', tokens: 9000, after_compact: false },
    { turn: 3, at: 'c', tokens: 2000, after_compact: true },
  ],
  top: [{ tool: 'Read', key: 'Read big.go', tokens: 8000, in_tokens: 100, rereads: 3, weight: 24000, at: 'b', subagent: true }],
}
const routes = { '/api/context/top': top, '/api/projects': [{ id: 1, name: 'Cockpit', path: '/x', capture: true, base_branch: 'main', parent_id: null, kind: 'repo' }], '/api/context/sessions/s1': detail }

describe('ContextSection', () => {
  it('lista chaves com valores formatados e o resto', async () => {
    mock(routes)
    show()
    expect(await screen.findByRole('heading', { name: 'O que enche o contexto' })).toBeInTheDocument()
    const row = (await screen.findAllByText('Read README.md'))[0].closest('tr')!
    expect(within(row).getAllByRole('cell').map((c) => c.textContent)).toEqual(['3', '500', '12 k', '4 k', '90 k'])
    expect(screen.getByText('hook SessionStart · cockpit')).toBeInTheDocument()
    const rest = screen.getByText('Resto (base e conversa)').closest('tr')!
    expect(rest).toHaveTextContent('105 k')
    expect(screen.getByText(/Peso = tamanho × turnos/)).toBeInTheDocument()
    expect(screen.getByText(/Estimativa: bytes ÷ 4/)).toBeInTheDocument()
    // As chaves listadas somam o peso total: sem linha "Outras chaves".
    expect(screen.queryByText('Outras chaves')).toBeNull()
  })

  it('linhas + Outras chaves + Resto somam o contexto total', async () => {
    // weight_total cobre chaves fora do top 30: 95 k listados, 145 k no total.
    mock({ ...routes, '/api/context/top': { ...top, context_total: 200000, weight_total: 145000 } })
    show()
    const others = (await screen.findByText('Outras chaves')).closest('tr')!
    expect(others).toHaveTextContent('50 k')
    const rest = screen.getByText('Resto (base e conversa)').closest('tr')!
    expect(rest).toHaveTextContent('55 k')
    // A linha Outras chaves fica logo acima do Resto.
    expect(others.nextElementSibling).toBe(rest)
    const weights = within(others.closest('tbody')!).getAllByRole('row').map((r) => {
      const cells = within(r).getAllByRole('cell')
      return cells[cells.length - 1].textContent
    })
    expect(weights).toEqual(['90 k', '5 k', '50 k', '55 k'])
    expect(90000 + 5000 + 50000 + 55000).toBe(200000)
  })

  it('resto nunca fica negativo', async () => {
    mock({ ...routes, '/api/context/top': { ...top, context_total: 10, weight_total: 95000 } })
    show()
    expect(await screen.findByText('Resto (base e conversa)')).toBeInTheDocument()
    expect(screen.getByText('Resto (base e conversa)').closest('tr')).toHaveTextContent(/conversa\)0$/)
  })

  it('Por projeto só aparece sem filtro de projeto', async () => {
    mock(routes)
    const { unmount } = show()
    expect(await screen.findByText('Por projeto')).toBeInTheDocument()
    expect(await screen.findByText('Cockpit')).toBeInTheDocument()
    unmount()
    mock(routes)
    show(1, true)
    await screen.findAllByText('Read README.md')
    expect(screen.queryByText('Por projeto')).toBeNull()
    expect(urls.find((u) => u.startsWith('/api/context/top'))).toContain('project=1&exact=1')
  })

  it('clicar numa sessão mostra a curva e as saídas mais pesadas', async () => {
    mock(routes)
    show()
    expect(await screen.findByText('sem card')).toBeInTheDocument()
    const s1 = screen.getByRole('button', { name: /NAC-7/ })
    expect(within(s1).getByText('peso 80 k')).toBeInTheDocument()
    expect(within(s1).getByText('pico 150 k')).toBeInTheDocument()
    expect(within(s1).getByText('2 compactações')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /NAC-7/ }))
    expect(await screen.findByRole('img', { name: 'Contexto por turno' })).toBeInTheDocument()
    expect(screen.getByText('Saídas mais pesadas')).toBeInTheDocument()
    expect(screen.getByText('8 k × 3 releituras')).toBeInTheDocument()
    expect(screen.getByText('subagente')).toBeInTheDocument()
    expect(screen.getAllByTestId('compact')).toHaveLength(1)
    expect(urls).toContain('/api/context/sessions/s1')
    fireEvent.click(screen.getByRole('button', { name: /NAC-7/ }))
    expect(screen.queryByRole('img', { name: 'Contexto por turno' })).toBeNull()
  })

  it('sessão só com % mostra "pico N%" na lista, mesmo sem saídas no período', async () => {
    mock({ ...routes, '/api/context/top': { keys: [], projects: [], context_total: 0, weight_total: 0, sessions: [
      { session_id: 'k1', card_key: 'NAC-9', card_title: 'Kiro', started_at: '2026-11-15T12:00:00Z', peak_context: 0, peak_pct: 41.6, compactions: 1, weight: 0 },
    ] } })
    show()
    const k1 = await screen.findByRole('button', { name: /NAC-9/ })
    expect(within(k1).getByText('pico 42%')).toBeInTheDocument()
    expect(within(k1).getByText('1 compactação')).toBeInTheDocument()
    expect(screen.getByText('Nenhuma saída medida no período.')).toBeInTheDocument()
  })

  it('sem dados', async () => {
    mock({ ...routes, '/api/context/top': { keys: null, sessions: null, projects: null, context_total: 0, weight_total: 0 } })
    show()
    expect(await screen.findByText('Nenhuma saída medida no período.')).toBeInTheDocument()
  })

  it('sessão só com %: curva em % com "janela informada pelo agente"', async () => {
    mock({ ...routes, '/api/context/sessions/s1': { turns: [
      { turn: 0, at: 'a', tokens: 0, after_compact: false, pct: 12, pct_only: true },
      { turn: 1, at: 'b', tokens: 0, after_compact: false, pct: 37, pct_only: true },
    ], top: [] } })
    show()
    fireEvent.click(await screen.findByRole('button', { name: /NAC-7/ }))
    expect(await screen.findByText('janela informada pelo agente')).toBeInTheDocument()
    expect(screen.getByText('pico 37%')).toBeInTheDocument()
  })

  it('sessão com tokens não mostra o aviso de janela informada', async () => {
    mock(routes)
    show()
    fireEvent.click(await screen.findByRole('button', { name: /NAC-7/ }))
    expect(await screen.findByText('pico 9 k')).toBeInTheDocument()
    expect(screen.queryByText('janela informada pelo agente')).toBeNull()
  })
})
