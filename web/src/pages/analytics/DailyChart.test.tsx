import { fireEvent, render, screen } from '@testing-library/react'
import { DailyChart } from './DailyChart'
import { buildDays } from './data'
import type { Total } from '../../api/types'
import { CurrencyContext } from '../../ui/money'

const t = (day: string, agent: string, cost: number, unpriced = false): Total =>
  ({ key: `${day}|${agent}`, agent, day, sessions: 1, input: 0, output: 0, cache_write: 0, cache_read: 0, cost_usd: cost, credits: 0, unpriced })

describe('DailyChart', () => {
  const days = buildDays([t('2026-11-04', 'claude', 6.9), t('2026-11-04', 'codex', 6.3), t('2026-11-05', 'kiro', 0.69)], '2026-11-04', '2026-11-06')

  it('renderiza um grupo por dia com o total no aria-label', () => {
    render(<DailyChart days={days} agents={['claude', 'codex', 'kiro']} />)
    const groups = screen.getAllByRole('img')
    expect(groups).toHaveLength(2)
    expect(groups[0]).toHaveAttribute('aria-label', expect.stringContaining('total US$ 13,20'))
    expect(groups[1]).toHaveAttribute('aria-label', expect.stringContaining('Kiro US$ 0,69'))
  })

  it('mostra o tooltip ao focar pelo teclado', () => {
    render(<DailyChart days={days} agents={['claude', 'codex', 'kiro']} />)
    expect(screen.queryByRole('tooltip')).toBeNull()
    fireEvent.focus(screen.getAllByRole('img')[0])
    expect(screen.getByRole('tooltip')).toHaveTextContent('Claude Code US$ 6,90')
  })

  it('uso sem preço aparece como "sem preço", nunca zero', () => {
    const d = buildDays([t('2026-11-04', 'codex', 0, true)], '2026-11-04', '2026-11-05')
    render(<DailyChart days={d} agents={['codex']} />)
    expect(screen.getByRole('img')).toHaveAttribute('aria-label', expect.stringContaining('Codex sem preço'))
  })

  it('respeita BRL da configuração nos rótulos', () => {
    render(<CurrencyContext.Provider value={{ currency: 'BRL', rate: 5 }}><DailyChart days={days} agents={['claude', 'codex', 'kiro']} /></CurrencyContext.Provider>)
    expect(screen.getAllByRole('img')[0]).toHaveAttribute('aria-label', expect.stringContaining('total R$ 66,00'))
  })
})
