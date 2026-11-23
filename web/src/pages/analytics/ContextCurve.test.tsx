import { render, screen } from '@testing-library/react'
import { ContextCurve } from './ContextCurve'

describe('ContextCurve', () => {
  it('sem turnos não quebra e não desenha pico', () => {
    render(<ContextCurve turns={[]} />)
    expect(screen.getByRole('img', { name: 'Contexto por turno' })).toBeInTheDocument()
    expect(screen.queryByText(/pico/)).toBeNull()
  })
  it('rotula o pico e marca compactações', () => {
    render(<ContextCurve turns={[
      { turn: 1, at: 'a', tokens: 1000, after_compact: false },
      { turn: 2, at: 'b', tokens: 150000, after_compact: false },
      { turn: 3, at: 'c', tokens: 2000, after_compact: true },
    ]} />)
    expect(screen.getByText('pico 150 k')).toBeInTheDocument()
    expect(screen.getAllByTestId('compact')).toHaveLength(1)
  })
  it('só com %: eixo em % e rótulo do pico em %, sem tokens', () => {
    render(<ContextCurve turns={[
      { turn: 0, at: 'a', tokens: 0, after_compact: false, pct: 10, pct_only: true },
      { turn: 1, at: 'b', tokens: 0, after_compact: false, pct: 42, pct_only: true },
      { turn: 2, at: 'c', tokens: 0, after_compact: false, pct: 30, pct_only: true },
    ]} />)
    expect(screen.getByText('pico 42%')).toBeInTheDocument()
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.queryByText(/ k/)).toBeNull()
  })
})
