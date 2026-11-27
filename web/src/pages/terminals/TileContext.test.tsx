import { fireEvent, render, screen } from '@testing-library/react'
import { CtxChip, CtxStrip, CtxWarning } from './TileContext'

describe('CtxChip', () => {
  it('mostra ctx e o percentual, com o nível no atributo', () => {
    const { rerender } = render(<CtxChip pct={62} limit={80} />)
    const chip = screen.getByTestId('ctx-chip')
    expect(chip).toHaveTextContent('ctx 62%')
    expect(chip).toHaveAttribute('title', 'Contexto usado')
    expect(chip).toHaveAttribute('data-level', 'ok')
    rerender(<CtxChip pct={84} limit={80} />)
    expect(screen.getByTestId('ctx-chip')).toHaveAttribute('data-level', 'warn')
    rerender(<CtxChip pct={92} limit={80} />)
    expect(screen.getByTestId('ctx-chip')).toHaveAttribute('data-level', 'crit')
  })
})

describe('CtxStrip', () => {
  const base = { pct: 62, tokens: 124000, windowTokens: 200000, pctOnly: false, limit: 80 }

  it('mostra percentual, detalhe, barra e traço do limite', () => {
    render(<CtxStrip {...base} />)
    const strip = screen.getByTestId('ctx-strip')
    expect(strip.querySelector('strong')).toHaveTextContent('Contexto 62%')
    expect(strip).toHaveTextContent('· 124 mil de 200 mil')
    const bar = screen.getByRole('progressbar', { name: 'Contexto usado' })
    expect(bar).toHaveAttribute('aria-valuenow', '62')
    const fill = screen.getByTestId('ctx-bar')
    expect(fill.style.width).toBe('62%')
    expect(fill).toHaveAttribute('data-level', 'ok')
    const mark = screen.getByTestId('ctx-limit')
    expect(mark.style.left).toBe('80%')
    expect(mark).toHaveAttribute('title', 'Limite configurado 80%')
  })

  it('pctOnly mostra só a porcentagem, sem tokens', () => {
    render(<CtxStrip {...base} pctOnly windowTokens={0} />)
    const strip = screen.getByTestId('ctx-strip')
    expect(strip).toHaveTextContent('· o agente informa só a %')
    expect(strip.textContent).not.toMatch(/mil/)
  })

  it('acima de 100 trava a barra em 100% e mantém o número como veio', () => {
    render(<CtxStrip {...base} pct={104} />)
    expect(screen.getByTestId('ctx-strip').querySelector('strong')).toHaveTextContent('Contexto 104%')
    expect(screen.getByTestId('ctx-bar').style.width).toBe('100%')
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100')
  })

  it('sem onClear não há botão Limpar; com onClear o clique chama uma vez', () => {
    const { rerender } = render(<CtxStrip {...base} />)
    expect(screen.queryByRole('button', { name: 'Limpar' })).toBeNull()
    const onClear = vi.fn()
    rerender(<CtxStrip {...base} onClear={onClear} />)
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    expect(onClear).toHaveBeenCalledOnce()
  })
})

describe('CtxWarning', () => {
  it('detalhado: âmbar com o texto completo', () => {
    render(<CtxWarning pct={84} limit={80} simple={false} onClear={vi.fn()} />)
    const w = screen.getByTestId('ctx-warning')
    expect(w).toHaveAttribute('role', 'status')
    expect(w).toHaveAttribute('data-level', 'warn')
    expect(w).toHaveTextContent('Contexto em 84%, acima do limite de 80%. Limpe e continue de onde parou.')
  })

  it('simples: texto curto', () => {
    render(<CtxWarning pct={84} limit={80} simple onClear={vi.fn()} />)
    expect(screen.getByTestId('ctx-warning')).toHaveTextContent('Contexto em 84%. Limpe e continue de onde parou.')
  })

  it('92 com limite 80 é crítico; o botão chama onClear', () => {
    const onClear = vi.fn()
    render(<CtxWarning pct={92} limit={80} simple={false} onClear={onClear} />)
    expect(screen.getByTestId('ctx-warning')).toHaveAttribute('data-level', 'crit')
    fireEvent.click(screen.getByRole('button', { name: 'Limpar e continuar' }))
    expect(onClear).toHaveBeenCalledOnce()
  })
})
