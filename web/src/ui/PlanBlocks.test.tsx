import { render, screen } from '@testing-library/react'
import { PlanBlocks } from './PlanBlocks'
import type { PlanTask } from '../api/types'

const t = (n: number, status: PlanTask['status'], title: string): PlanTask => ({ n, title, status, session_id: '', updated_at: '2026-11-10T12:00:00Z' })

describe('PlanBlocks', () => {
  it('mostra N/M, um bloco por tarefa com title e o aria-label do conjunto', () => {
    render(<PlanBlocks tasks={[t(1, 'concluida', 'api'), t(2, 'em_andamento', 'filtros'), t(3, 'em_revisao', 'docs'), t(4, 'a_fazer', 'deploy')]} />)
    expect(screen.getByText('1/4 concluídas')).toBeInTheDocument()
    const img = screen.getByRole('img', { name: 'Plano: 1 de 4 tarefas concluídas' })
    expect(img).toBeInTheDocument()
    for (const title of ['Tarefa 1: api — concluída', 'Tarefa 2: filtros — em andamento', 'Tarefa 3: docs — em revisão', 'Tarefa 4: deploy — a fazer'])
      expect(document.querySelector(`[title="${title}"]`)).not.toBeNull()
  })
})
