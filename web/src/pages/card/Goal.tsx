import type { CardDetail } from '../../api/types'
import { EditableText } from './EditableText'
import { h2, muted, panel } from './shared'

export function Goal({ card }: { card: CardDetail }) {
  const f = (field: 'goal' | 'done_when' | 'how_to_test', label: string, title: string) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <strong style={{ ...muted, fontWeight: 600 }}>{title}</strong>
      <EditableText cardKey={card.key} field={field} label={label} value={card[field]} />
    </div>
  )
  return (
    <section aria-labelledby="obj-t" style={panel}>
      <h2 id="obj-t" style={h2}>Objetivo</h2>
      {f('goal', 'objetivo', 'Objetivo')}
      {f('done_when', 'pronto quando', 'Pronto quando')}
      {f('how_to_test', 'como testar', 'Como testar')}
    </section>
  )
}
