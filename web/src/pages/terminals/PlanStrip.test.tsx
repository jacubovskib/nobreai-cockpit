import { render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PlanStrip } from './PlanStrip'

function mock(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    if (r === undefined) return new Response('{}', { status: 404 })
    if (r && typeof r === 'object' && 'status' in r && 'body' in r) {
      const o = r as { status: number; body: unknown }
      return new Response(JSON.stringify(o.body), { status: o.status })
    }
    return new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const inH = (h: number) => new Date(Date.now() + h * 3600_000).toISOString()
const win = (used: number, o: Record<string, unknown> = {}) => ({ used_pct: used, resets_at: inH(2), received_at: new Date().toISOString(), expired: false, ...o })
const plan = (five: unknown, seven: unknown, warn = 80) => ({ five_hour: five, seven_day: seven, history: { five_hour: null, seven_day: null }, warn_pct: warn })
const overview = (credits: number) => ({ today: { per_agent: [{ agent: 'kiro', credits }] } })
const PLAN = '/api/plan-limits?hours=24'

describe('PlanStrip', () => {
  it('detalhado: cartão Claude com medidores, tons e reinício', async () => {
    mock({ [PLAN]: plan(win(62), win(41)), '/api/overview': overview(0) })
    wrap(<PlanStrip simple={false} hasKiro={false} />)
    const strip = await screen.findByRole('region', { name: 'Uso do plano' })
    expect(within(strip).getByText('Claude')).toBeInTheDocument()
    const m5 = within(strip).getByRole('meter', { name: 'Sessão de 5 h: 62% usado' })
    expect(m5).toHaveAttribute('data-tone', 'ok')
    expect(within(strip).getByRole('meter', { name: 'Semana: 41% usado' })).toBeInTheDocument()
    expect(within(strip).getAllByText(/^reinicia em/)).toHaveLength(2)
  })
  it('detalhado: tons warn e crit', async () => {
    mock({ [PLAN]: plan(win(85), win(92)), '/api/overview': overview(0) })
    wrap(<PlanStrip simple={false} hasKiro={false} />)
    expect(await screen.findByRole('meter', { name: 'Sessão de 5 h: 85% usado' })).toHaveAttribute('data-tone', 'warn')
    expect(screen.getByRole('meter', { name: 'Semana: 92% usado' })).toHaveAttribute('data-tone', 'crit')
  })
  it('simples: chips, sem medidores', async () => {
    mock({ [PLAN]: plan(win(62), win(41)), '/api/overview': overview(12.34) })
    wrap(<PlanStrip simple hasKiro />)
    expect(await screen.findByText('Claude 5 h 62% · sem 41%')).toHaveAttribute('data-tone', 'ok')
    expect(screen.getByText('Kiro 12,3 créditos hoje')).toBeInTheDocument()
    expect(screen.queryByRole('meter')).toBeNull()
  })
  it('simples: o chip leva o pior tom', async () => {
    mock({ [PLAN]: plan(win(62), win(95)), '/api/overview': overview(0) })
    wrap(<PlanStrip simple hasKiro={false} />)
    expect(await screen.findByText('Claude 5 h 62% · sem 95%')).toHaveAttribute('data-tone', 'crit')
  })
  it('janela vencida mostra sem leitura recente e nenhum percentual dela', async () => {
    mock({ [PLAN]: plan(win(62, { expired: true }), win(41)), '/api/overview': overview(0) })
    wrap(<PlanStrip simple={false} hasKiro={false} />)
    expect(await screen.findByText('sem leitura recente')).toBeInTheDocument()
    expect(screen.queryByText('62%')).toBeNull()
    expect(screen.queryByRole('meter', { name: /Sessão de 5 h/ })).toBeNull()
    expect(screen.getByRole('meter', { name: 'Semana: 41% usado' })).toBeInTheDocument()
  })
  it('sem dado do Claude e sem Kiro: nada renderizado', async () => {
    mock({ [PLAN]: plan(null, null), '/api/overview': overview(0) })
    const { container } = wrap(<PlanStrip simple={false} hasKiro />)
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(2))
    await new Promise((r) => setTimeout(r, 20))
    expect(container.firstChild).toBeNull()
  })
  it.each([
    ['500', { status: 500, body: { error: 'x' } }],
    ['lista', []],
  ])('respostas %s: nada renderizado, sem erro', async (_n, bad) => {
    mock({ [PLAN]: bad, '/api/overview': bad })
    const { container } = wrap(<PlanStrip simple={false} hasKiro />)
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(2))
    await new Promise((r) => setTimeout(r, 20))
    expect(container.firstChild).toBeNull()
  })
})
