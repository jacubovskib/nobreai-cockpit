import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PlanLimits } from './PlanLimits'
import { ControlRoom } from './ControlRoom'
import { Config } from './Config'
import type { PlanLimits as PlanData, Overview } from '../api/types'

const NOW = new Date('2026-11-07T15:16:00')
const iso = (minAhead: number) => new Date(NOW.getTime() + minAhead * 60000).toISOString()
const hhmm = (d: Date) => `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{}', { status: 404 })
    return new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
beforeEach(() => vi.useFakeTimers({ toFake: ['Date'], now: NOW }))
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

const data = (o: Partial<PlanData>): PlanData => ({ five_hour: null, seven_day: null, history: { five_hour: [], seven_day: [] }, warn_pct: 80, ...o })
const URL = '/api/plan-limits?hours=24'

describe('PlanLimits', () => {
  it('mostra percentuais, reinício, atualização e marcador', async () => {
    const reset5 = iso(134), resetW = iso(60 * 30)
    mock({ [URL]: data({
      five_hour: { used_pct: 42.4, resets_at: reset5, received_at: iso(-3), expired: false },
      seven_day: { used_pct: 85, resets_at: resetW, received_at: iso(-3), expired: false },
      warn_pct: 70,
    }) })
    wrap(<PlanLimits />)
    expect(await screen.findByText('42%')).toBeInTheDocument()
    expect(screen.getByText(/85%/)).toBeInTheDocument()
    expect(screen.getByText(new RegExp(`reinicia em 2 h 14 min \\(às ${hhmm(new Date(reset5))}\\)`))).toBeInTheDocument()
    const wd = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'][new Date(resetW).getDay()]
    expect(screen.getByText(new RegExp(`reinicia em 1 d 6 h \\(${wd} às ${hhmm(new Date(resetW))}\\)`))).toBeInTheDocument()
    expect(screen.getAllByText(/atualizado há 3 min/)).toHaveLength(2)
    expect(screen.getByTestId('plan-warn-marker-five_hour').style.left).toBe('70%')
    expect(screen.getByRole('meter', { name: 'Sessão de 5 h: 42% usado' })).toHaveAttribute('aria-valuenow', '42')
    expect(screen.getByText(/· aviso/)).toBeInTheDocument()
  })

  it('janela ausente diz "sem dados ainda", nunca 0%', async () => {
    mock({ [URL]: data({ five_hour: { used_pct: 10, resets_at: iso(60), received_at: iso(-1), expired: false } }) })
    wrap(<PlanLimits />)
    expect(await screen.findByText('sem dados ainda')).toBeInTheDocument()
    expect(screen.queryByText('0%')).not.toBeInTheDocument()
  })

  it('janela expirada pede nova leitura', async () => {
    mock({ [URL]: data({ five_hour: { used_pct: 90, resets_at: iso(-5), received_at: iso(-60), expired: true } }) })
    wrap(<PlanLimits />)
    expect(await screen.findByText('reiniciada — aguardando nova leitura')).toBeInTheDocument()
    expect(screen.queryByText('90%')).not.toBeInTheDocument()
  })

  it('resets_at já passado no relógio do cliente conta como expirada', async () => {
    mock({ [URL]: data({ five_hour: { used_pct: 90, resets_at: iso(-1), received_at: iso(-30), expired: false } }) })
    wrap(<PlanLimits />)
    expect(await screen.findByText('reiniciada — aguardando nova leitura')).toBeInTheDocument()
    expect(screen.queryByText('90%')).not.toBeInTheDocument()
    expect(screen.queryByRole('meter')).not.toBeInTheDocument()
  })

  it('sem nenhuma leitura explica a status line e linka para Configuração', async () => {
    mock({ [URL]: data({}) })
    wrap(<PlanLimits />)
    expect(await screen.findByText(/Sem dados do plano ainda/)).toBeInTheDocument()
    expect(screen.getByText(/status line do Claude Code/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Configuração' })).toHaveAttribute('href', '/config#agentes')
    expect(screen.queryByText(/%/)).not.toBeInTheDocument()
  })

  it('sparkline descreve mínimo, máximo e último', async () => {
    mock({ [URL]: data({
      five_hour: { used_pct: 40, resets_at: iso(60), received_at: iso(-1), expired: false },
      history: { five_hour: [{ at: iso(-120), used_pct: 12 }, { at: iso(-60), used_pct: 55 }, { at: iso(-1), used_pct: 40 }], seven_day: null },
    }) })
    wrap(<PlanLimits />)
    const img = await screen.findByRole('img', { name: /Sessão de 5 h, últimas 24 h: mínimo 12%, máximo 55%, último 40%/ })
    expect(img.querySelector('polyline')).not.toBeNull()
  })

  it('erro da API não derruba o painel', async () => {
    mock({})
    wrap(<PlanLimits />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Não foi possível carregar os limites do plano')
  })
})

describe('alerta plan_limit na Sala de controle', () => {
  it('renderiza com link para o painel, sem card nem agente', async () => {
    const overview: Overview = {
      active: [], no_card: [], events: [],
      today: { active_agents: 0, cards_in_progress: 0, cards_without_live_session: 0, cost_usd: 0, cost_unpriced: false, daily_limit_usd: 0, tokens_input: 0, tokens_output: 0, tokens_cache_write: 0, tokens_cache_read: 0, per_agent: [] },
      alerts: [{ kind: 'plan_limit', card_key: '', agent: 'claude', text: 'Sessão de 5 h do plano em 85% (reinicia às 17:30).' }],
    }
    mock({ '/api/overview': overview, '/api/settings': {}, [URL]: data({}), '/api/terminals': [] })
    wrap(<ControlRoom />)
    expect(await screen.findByText(/Sessão de 5 h do plano em 85%/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Ver limites do plano' })).toHaveAttribute('href', '#plano-limites')
    expect(screen.queryByRole('link', { name: 'Abrir card' })).not.toBeInTheDocument()
    expect(await screen.findByRole('region', { name: 'Limites do plano' })).toBeInTheDocument()
  })

  it('some quando a janela já reiniciou no relógio do cliente', async () => {
    const overview: Overview = {
      active: [], no_card: [], events: [],
      today: { active_agents: 0, cards_in_progress: 0, cards_without_live_session: 0, cost_usd: 0, cost_unpriced: false, daily_limit_usd: 0, tokens_input: 0, tokens_output: 0, tokens_cache_write: 0, tokens_cache_read: 0, per_agent: [] },
      alerts: [
        { kind: 'plan_limit', card_key: '', agent: 'claude', text: 'Sessão de 5 h do plano em 85% (reinicia às 15:15).', resets_at: iso(-1) },
        { kind: 'plan_limit', card_key: '', agent: 'claude', text: 'Limite semanal do plano em 90% (reinicia ter às 19:05).', resets_at: iso(60 * 24) },
      ],
    }
    mock({ '/api/overview': overview, '/api/settings': {}, [URL]: data({}), '/api/terminals': [] })
    wrap(<ControlRoom />)
    expect(await screen.findByText(/Limite semanal do plano em 90%/)).toBeInTheDocument()
    expect(screen.queryByText(/Sessão de 5 h do plano em 85%/)).not.toBeInTheDocument()
  })

  it('sem alertas vigentes diz que nada precisa de atenção', async () => {
    const overview: Overview = {
      active: [], no_card: [], events: [],
      today: { active_agents: 0, cards_in_progress: 0, cards_without_live_session: 0, cost_usd: 0, cost_unpriced: false, daily_limit_usd: 0, tokens_input: 0, tokens_output: 0, tokens_cache_write: 0, tokens_cache_read: 0, per_agent: [] },
      alerts: [{ kind: 'plan_limit', card_key: '', agent: 'claude', text: 'Sessão de 5 h do plano em 85% (reinicia às 15:15).', resets_at: iso(-1) }],
    }
    mock({ '/api/overview': overview, '/api/settings': {}, [URL]: data({}), '/api/terminals': [] })
    wrap(<ControlRoom />)
    expect(await screen.findByText('Nada precisa de atenção agora.')).toBeInTheDocument()
  })
})

describe('Config: aviso do plano', () => {
  const settings = {
    rules: { goal: true }, where_stopped_ttl_min: 15, grace_min: 15, reminder_edit_threshold: 3, launch_mode: 'terminal', terminal_cmd: [],
    currency: 'USD', brl_rate: 0, incomplete_action: 'ask_agent', plan_warn_pct: 80, demo: false,
  }
  const routes = {
    '/api/settings': settings, '/api/limits': [], '/api/projects': [], '/api/cards': [], '/api/prices': [], '/api/usage/totals?group=model': [],
    '/api/agents': [{ agent: 'claude', detection: { found: true, path: '/usr/bin/claude', version: '2', checks: [{ key: 'statusline', label: 'Status line integrada', detail: '', ok: false }] } }],
    'PUT /api/settings': settings,
  }
  it('mostra o check da status line e salva plan_warn_pct', async () => {
    const fn = mock(routes)
    wrap(<Config />)
    expect(await screen.findByText('Status line integrada')).toBeInTheDocument()
    const input = await screen.findByLabelText(/Avisar quando o limite do plano passar de/)
    expect(input).toHaveValue('80')
    fireEvent.change(input, { target: { value: '90' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar aviso do plano' }))
    await waitFor(() => expect(fn.mock.calls.some(([u, i]) => u === '/api/settings' && i?.method === 'PUT')).toBe(true))
    const put = fn.mock.calls.find(([u, i]) => u === '/api/settings' && i?.method === 'PUT')!
    expect(JSON.parse(put[1]!.body!).plan_warn_pct).toBe(90)
  })
  it('rejeita percentual fora de 1–100 sem enviar', async () => {
    const fn = mock(routes)
    wrap(<Config />)
    fireEvent.change(await screen.findByLabelText(/Avisar quando o limite do plano passar de/), { target: { value: '150' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar aviso do plano' }))
    expect(await screen.findByText('Informe um percentual inteiro de 1 a 100.')).toBeInTheDocument()
    expect(fn.mock.calls.some(([, i]) => i?.method === 'PUT')).toBe(false)
  })
})
