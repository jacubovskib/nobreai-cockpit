import { useQuery } from '@tanstack/react-query'
import { get } from '../../api/client'
import { ErrorLine, h2, muted, panel } from './shared'

export function ResumePreview({ cardKey }: { cardKey: string }) {
  const q = useQuery({ queryKey: ['card', cardKey, 'resume'], queryFn: () => get<{ markdown: string }>(`/api/cards/${cardKey}/resume`) })
  return (
    <section id="pacote" aria-labelledby="pac-t" style={panel}>
      <h2 id="pac-t" style={h2}>Pacote de retomada (prévia)</h2>
      {q.isLoading && <p style={{ ...muted, margin: 0 }}>Gerando…</p>}
      <ErrorLine error={q.error} />
      {q.data && <pre className="mono" style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 13 }}>{q.data.markdown}</pre>}
    </section>
  )
}
