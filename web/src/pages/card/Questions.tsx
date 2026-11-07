import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../../api/client'
import type { CardDetail, Question } from '../../api/types'
import { ErrorLine, btnPrimary, h2, input, muted, panel } from './shared'

function QuestionRow({ q, cardKey }: { q: Question; cardKey: string }) {
  const qc = useQueryClient()
  const [answer, setAnswer] = useState('')
  const save = useMutation({
    mutationFn: () => patch(`/api/questions/${q.id}`, { answer }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['card', cardKey] }),
  })
  const open = q.answer === ''
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      <span>{q.text}</span>
      {open ? (
        <form style={{ display: 'flex', gap: 8 }} onSubmit={(e) => { e.preventDefault(); if (answer.trim()) save.mutate() }}>
          <input style={{ ...input, flex: 1 }} aria-label={`Responder pergunta: ${q.text}`} placeholder="Responder…" value={answer} onChange={(e) => setAnswer(e.target.value)} />
          <button type="submit" style={btnPrimary} disabled={save.isPending}>Responder</button>
        </form>
      ) : (
        <span style={muted}>Resposta: {q.answer}</span>
      )}
      <ErrorLine error={save.error} />
    </div>
  )
}

export function Questions({ card }: { card: CardDetail }) {
  const qs = card.content.questions
  return (
    <section aria-labelledby="blq-t" style={panel}>
      <h2 id="blq-t" style={h2}>Perguntas em aberto</h2>
      {qs.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhuma pergunta.</p>}
      {qs.map((q) => <QuestionRow key={q.id} q={q} cardKey={card.key} />)}
    </section>
  )
}
