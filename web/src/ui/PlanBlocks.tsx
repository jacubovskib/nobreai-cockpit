import type { PlanTask, TaskStatus } from '../api/types'

export const TASK_LABEL: Record<TaskStatus, string> = {
  a_fazer: 'a fazer',
  em_andamento: 'em andamento',
  em_revisao: 'em revisão',
  concluida: 'concluída',
}

const blockStyle = (s: TaskStatus) => ({
  width: 14,
  height: 14,
  borderRadius: 3,
  boxSizing: 'border-box' as const,
  border: `2px solid ${s === 'concluida' ? '#0CA30C' : s === 'a_fazer' ? 'var(--border)' : '#FAB219'}`,
  background: s === 'concluida' ? '#0CA30C' : s === 'em_andamento' ? '#FAB219' : 'transparent',
})

// Um bloco por tarefa; o N/M em texto acompanha, a cor nunca é a única informação.
export function PlanBlocks({ tasks }: { tasks: PlanTask[] }) {
  const done = tasks.filter((t) => t.status === 'concluida').length
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
      <span style={{ fontSize: 12.5, color: 'var(--text-2)' }}>{done}/{tasks.length} concluídas</span>
      <span role="img" aria-label={`Plano: ${done} de ${tasks.length} tarefas concluídas`} style={{ display: 'inline-flex', gap: 3, flexWrap: 'wrap' }}>
        {tasks.map((t) => (
          <span key={t.n} title={`Tarefa ${t.n}: ${t.title} — ${TASK_LABEL[t.status]}`} style={blockStyle(t.status)} />
        ))}
      </span>
    </span>
  )
}
