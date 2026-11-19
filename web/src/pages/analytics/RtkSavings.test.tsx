import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RtkSavings } from './RtkSavings'

const urls: string[] = []
function mock(body: unknown) {
  urls.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(url)
    return new Response(JSON.stringify(body), { status: 200 })
  }))
}
function show(projectId: number | null = null, exact = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter><RtkSavings from="2026-11-11" to="2026-11-18" projectId={projectId} exact={exact} /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const gain = { available: true, commands: 120, input_tokens: 900000, saved_tokens: 700000, savings_pct: 77.8, oldest_day: '2026-10-01' }
const sv = {
  gain,
  compare: [
    { key: 'git status', before_count: 10, before_avg: 4000, after_count: 8, after_avg: 520, change_pct: -87 },
    { key: 'ls', before_count: 4, before_avg: 100, after_count: 4, after_avg: 105, change_pct: 5 },
  ],
  activations: [{ on: '2026-11-01T10:00:00Z' }],
}

describe('RtkSavings', () => {
  it('mostra economia e tabela antes/depois', async () => {
    mock(sv)
    show(3, true)
    expect(await screen.findByText('Economia do RTK')).toBeInTheDocument()
    expect(await screen.findByText(/120 comandos/)).toBeInTheDocument()
    expect(screen.getByText(/700 k/)).toBeInTheDocument()
    expect(screen.getByText(/78%/)).toBeInTheDocument()
    expect(screen.getByText('Totais do RTK valem para todos os projetos.')).toBeInTheDocument()
    const row = screen.getByText('$ git status').closest('tr')!
    expect(within(row).getByText('−87%')).toBeInTheDocument()
    expect(screen.getByText('+5%')).toBeInTheDocument()
    expect(screen.getByText('O RTK não comprime cat (lê o arquivo inteiro) nem reescreve sed e python3.')).toBeInTheDocument()
    expect(urls[0]).toBe('/api/rtk/savings?from=2026-11-11&to=2026-11-18&project=3&exact=1')
  })

  it('indisponível', async () => {
    mock({ ...sv, gain: { ...gain, available: false } })
    show()
    expect(await screen.findByText('Economia do RTK indisponível agora')).toBeInTheDocument()
  })

  it('sem ativação aponta para a Configuração', async () => {
    mock({ ...sv, compare: [], activations: [] })
    show()
    expect(await screen.findByText(/RTK não está ativo/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Configuração/ })).toHaveAttribute('href', '/config')
  })

  it('histórico mais curto que o período', async () => {
    mock({ ...sv, gain: { ...gain, oldest_day: '2026-11-14' } })
    show()
    expect(await screen.findByText(/o RTK guarda 90 dias/)).toBeInTheDocument()
  })
})
