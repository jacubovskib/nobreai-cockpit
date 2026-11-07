import { render, screen } from '@testing-library/react'
import { Money } from './Money'
import { CurrencyContext } from './money'
import { Tokens } from './Tokens'
import { ReadinessPips } from './ReadinessPips'
import { AgentSwatch } from './AgentSwatch'
import { StatusBadge } from './StatusBadge'

describe('Money', () => {
  it('formata USD em pt-BR', () => {
    render(<Money usd={4.82} />)
    expect(screen.getByText('US$ 4,82')).toBeInTheDocument()
  })
  it('mostra "sem preço" quando não há preço', () => {
    render(<Money usd={0} unpriced />)
    expect(screen.getByText('sem preço')).toBeInTheDocument()
  })
  it('converte para BRL quando a moeda é BRL com cotação', () => {
    render(<Money usd={4} currency="BRL" rate={5.5} />)
    expect(screen.getByText('R$ 22,00')).toBeInTheDocument()
  })
  it('sem props usa a moeda do contexto (configuração)', () => {
    render(<CurrencyContext.Provider value={{ currency: 'BRL', rate: 5 }}><Money usd={2} /></CurrencyContext.Provider>)
    expect(screen.getByText('R$ 10,00')).toBeInTheDocument()
  })
  it('fica em USD se BRL sem cotação', () => {
    render(<Money usd={4} currency="BRL" rate={0} />)
    expect(screen.getByText('US$ 4,00')).toBeInTheDocument()
  })
})

describe('Tokens', () => {
  it.each([
    [1_082_000, '1,08 M'],
    [412_000, '412 k'],
    [999, '999'],
    [0, '0'],
  ])('%i -> %s', (n, text) => {
    render(<Tokens n={n} />)
    expect(screen.getByText(text)).toBeInTheDocument()
  })
})

describe('ReadinessPips', () => {
  it('expõe aria-label com a contagem', () => {
    render(<ReadinessPips met={4} total={5} />)
    expect(screen.getByLabelText('4/5 para handoff')).toBeInTheDocument()
  })
})

describe('AgentSwatch', () => {
  it('mostra o nome do agente ao lado da cor', () => {
    render(<AgentSwatch agent="codex" />)
    expect(screen.getByText('Codex')).toBeInTheDocument()
  })
})

describe('StatusBadge', () => {
  it('mostra rótulo da coluna e "Ao vivo" quando live', () => {
    render(<StatusBadge column="em_andamento" live />)
    expect(screen.getByText(/Ao vivo/)).toBeInTheDocument()
  })
  it('rótulo da coluna sem sessão viva', () => {
    render(<StatusBadge column="revisao" />)
    expect(screen.getByText('Revisão')).toBeInTheDocument()
  })
})
