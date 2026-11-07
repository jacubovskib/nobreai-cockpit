import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { forCard, isLive, useTerminals } from '../../api/terminals'
import { agentInfo } from '../../ui/agents'
import { LazyTerminalView } from '../../ui/LazyTerminalView'
import { h2, muted, panel } from './shared'

export const TERMINAL_ANCHOR = 'terminal'

// Painel "Terminal": o terminal vivo (ou recém-encerrado) do card, o mais novo
// primeiro. `focusId` é o terminal recém-iniciado em segundo plano.
export function CardTerminal({ cardKey, focusId }: { cardKey: string; focusId?: string }) {
  const { data } = useTerminals()
  const list = forCard(data, cardKey)
  const current = list.find((t) => t.id === focusId) ?? list[0]
  const ref = useRef<HTMLElement>(null)
  const visible = !!current
  useEffect(() => {
    if (focusId && visible) ref.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
  }, [focusId, visible])
  if (!current) return null
  return (
    <section ref={ref} id={TERMINAL_ANCHOR} aria-labelledby="term-t" style={panel}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h2 id="term-t" style={h2}>Terminal</h2>
        <span style={muted}>
          {agentInfo(current.agent).name} · {isLive(current) ? 'ao vivo' : 'encerrado'}
          {list.length > 1 && <> · <Link to="/terminals">{list.length} terminais deste card</Link></>}
        </span>
      </div>
      <div style={{ height: 460 }}>
        <LazyTerminalView key={current.id} id={current.id} readonly={!isLive(current)} />
      </div>
    </section>
  )
}
