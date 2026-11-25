import type { CardDetail } from '../../api/types'
import { agentInfo } from '../../ui/agents'
import { EditableText } from './EditableText'
import { h2, muted, panel } from './shared'
import { minutesSince } from '../boardModel'

function ago(iso: string | null) {
  if (!iso) return ''
  const m = minutesSince(iso)
  if (m < 1) return 'agora'
  if (m < 60) return `há ${Math.round(m)} min`
  if (m < 1440) return `há ${Math.round(m / 60)} h`
  return `há ${Math.round(m / 1440)} d`
}

export function WhereStopped({ card }: { card: CardDetail }) {
  const last = [...card.sessions].sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at)).at(-1)
  const meta = [last ? agentInfo(last.agent).name : '', ago(card.where_stopped_at)].filter(Boolean).join(' · ')
  return (
    <section aria-labelledby="onde-t" style={panel}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
        <h2 id="onde-t" style={h2}>Onde parei</h2>
        {meta && <span style={muted}>{meta}</span>}
      </div>
      <EditableText cardKey={card.key} field="where_stopped" label="onde parei" value={card.where_stopped} empty="Nenhum registro ainda. O agente preenche ao pausar; você também pode escrever." />
    </section>
  )
}
