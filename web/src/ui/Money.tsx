import { formatMoney, useCurrency } from './money'

// Money mostra custo em USD ("US$ 4,82"); com currency "BRL" e cotação > 0
// converte para "R$ x,xx". Sem currency/rate nas props usa a configuração
// (CurrencyContext). unpriced nunca vira zero: mostra "sem preço".
// Credits mostra o gasto em créditos ("12,50 créditos") ao lado do custo em US$,
// nunca somado a ele. Sem créditos (<= 0) não renderiza nada.
export function Credits({ credits }: { credits?: number }) {
  if (!credits || credits <= 0) return null
  return <span style={{ color: 'var(--text-2)' }}> · {credits.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} créditos</span>
}

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
