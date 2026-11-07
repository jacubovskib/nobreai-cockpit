import { formatMoney, useCurrency } from './money'

// Money mostra custo em USD ("US$ 4,82"); com currency "BRL" e cotação > 0
// converte para "R$ x,xx". Sem currency/rate nas props usa a configuração
// (CurrencyContext). unpriced nunca vira zero: mostra "sem preço".
export function Money({
  usd,
  unpriced,
  currency,
  rate,
}: {
  usd: number
  unpriced?: boolean
  currency?: string
  rate?: number
}) {
  const pref = useCurrency({ currency, rate })
  if (unpriced) return <span>sem preço</span>
  return <span>{formatMoney(usd, pref)}</span>
}
