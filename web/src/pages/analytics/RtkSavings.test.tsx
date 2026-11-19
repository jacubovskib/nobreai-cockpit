import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RtkSavings } from './RtkSavings'
import { ymd } from './data'

const urls: string[] = []
function mock(body: unknown) {
  urls.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.startsWith('/api/rtk/savings')) {
      urls.push(url)
      return new Response(JSON.stringify(body), { status: 200 })
    }
    if (url === '/api/projects') return new Response('[]', { status: 200 })
    return new Response('{}', { status: 404 })
  }))
}
function show(projectId: number | null = null, exact = false, from = '2026-11-11') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter><RtkSavings from={from} to="2026-11-18" projectId={projectId} exact={exact} /></MemoryRouter></QueryClientProvider>)
}
// daysAgo devolve o dia local de n dias atrás, para os testes não dependerem da data de hoje.
const daysAgo = (n: number) => {
  const t = new Date()
  return ymd(new Date(t.getFullYear(), t.getMonth(), t.getDate() - n))
}
afterEach(() => vi.unstubAllGlobals())

const gain = { available: true, commands: 120, input_tokens: 900000, saved_tokens: 700000, savings_pct: 77.8, oldest_day: '2026-10-01' }
const sv = {
  installed: true,
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
    expect(screen.getByRole('columnheader', { name: 'Antes (média/chamada)' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Depois (média/chamada)' })).toBeInTheDocument()
    const row = screen.getByText('$ git status').closest('tr')!
    expect(within(row).getByText('−87%')).toBeInTheDocument()
    expect(within(row).getByText('n=10')).toBeInTheDocument()
    expect(within(row).getByText('n=8')).toBeInTheDocument()
    expect(screen.getByText('+5%')).toBeInTheDocument()
    expect(screen.getByText('A comparação usa todo o histórico, não só o período.')).toBeInTheDocument()
    expect(screen.getByText('O RTK não comprime cat (lê o arquivo inteiro) nem reescreve sed e python3.')).toBeInTheDocument()
    expect(screen.queryByText(/RTK não está ativo/)).not.toBeInTheDocument()
    expect(screen.queryByText(/RTK guarda 90 dias|dados do RTK desde/i)).not.toBeInTheDocument()
    expect(urls[0]).toBe('/api/rtk/savings?from=2026-11-11&to=2026-11-18&project=3&exact=1')
  })

  it('instalado e gain indisponível', async () => {
    mock({ ...sv, gain: { ...gain, available: false } })
    show()
    expect(await screen.findByText('Economia do RTK indisponível agora')).toBeInTheDocument()
  })

  it('não instalado mostra só a linha curta com a Configuração', async () => {
    mock({ ...sv, installed: false, gain: { ...gain, available: false }, compare: [], activations: [] })
    show()
    expect(await screen.findByText(/RTK não instalado/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Configuração/ })).toHaveAttribute('href', '/config')
    expect(screen.queryByText('Economia do RTK indisponível agora')).not.toBeInTheDocument()
    expect(screen.queryByText(/RTK não está ativo/)).not.toBeInTheDocument()
  })

  it('sem ativação aponta para a Configuração', async () => {
    mock({ ...sv, compare: [], activations: [] })
    show()
    expect(await screen.findByText(/RTK não está ativo/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Configuração/ })).toHaveAttribute('href', '/config')
  })

  it('último intervalo fechado conta como inativo e mantém a tabela', async () => {
    mock({ ...sv, activations: [{ on: '2026-11-01T10:00:00Z', off: '2026-11-10T10:00:00Z' }] })
    show()
    expect(await screen.findByText(/RTK não está ativo/)).toBeInTheDocument()
    expect(screen.getByText('$ git status')).toBeInTheDocument()
  })

  it('período além de 90 dias cita o limite do RTK', async () => {
    mock({ ...sv, gain: { ...gain, oldest_day: daysAgo(89) } })
    show(null, false, daysAgo(120))
    expect(await screen.findByText(/o RTK guarda 90 dias/)).toBeInTheDocument()
  })

  it('histórico mais curto que um período recente mostra desde quando', async () => {
    const oldest = daysAgo(3)
    const [, m, d] = oldest.split('-')
    mock({ ...sv, gain: { ...gain, oldest_day: oldest } })
    show(null, false, daysAgo(7))
    expect(await screen.findByText(`Dados do RTK desde ${d}/${m}.`)).toBeInTheDocument()
    expect(screen.queryByText(/90 dias/)).not.toBeInTheDocument()
  })
})
