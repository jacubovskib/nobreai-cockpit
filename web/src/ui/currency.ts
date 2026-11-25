import { createContext, useContext } from 'react'

// Preferência de moeda de exibição (Configuração → Preços e moeda). O custo
// vem sempre em USD da API; com currency "BRL" e cotação > 0 é convertido.
export interface CurrencyPref { currency?: string; rate?: number }

// CurrencyContext é preenchido pelo Layout com as configurações; sem
// provedor (testes, componentes isolados) vale USD.
export const CurrencyContext = createContext<CurrencyPref>({})

const nf = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const isBRL = (p: CurrencyPref) => p.currency === 'BRL' && !!p.rate && p.rate > 0
export const moneySymbol = (p: CurrencyPref) => (isBRL(p) ? 'R$' : 'US$')
export const convertUSD = (usd: number, p: CurrencyPref) => (isBRL(p) ? usd * (p.rate as number) : usd)
// formatAmount: só o número, na moeda de exibição ("4,82").
export const formatAmount = (usd: number, p: CurrencyPref) => nf.format(convertUSD(usd, p))
// formatMoney: com símbolo ("US$ 4,82" ou "R$ 26,51").
export const formatMoney = (usd: number, p: CurrencyPref) => `${moneySymbol(p)} ${formatAmount(usd, p)}`

// useCurrency: props explícitas têm prioridade; o que faltar vem do contexto.
export function useCurrency(override: CurrencyPref = {}): CurrencyPref {
  const ctx = useContext(CurrencyContext)
  return { currency: override.currency ?? ctx.currency, rate: override.rate ?? ctx.rate }
}

// useMoney: formatadores de texto (rótulos, tooltips, eixos) na moeda configurada.
export function useMoney() {
  const p = useCurrency()
  return {
    symbol: moneySymbol(p),
    convert: (usd: number) => convertUSD(usd, p),
    amount: (usd: number) => formatAmount(usd, p),
    fmt: (usd: number) => formatMoney(usd, p),
  }
}
