import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProjects } from '../api/queries'
import { post } from '../api/client'
import { useProjectFilter } from '../app/project'

const field: React.CSSProperties = { minHeight: 40, padding: '0 10px', borderRadius: 8, border: '1px solid #2E333E', background: 'var(--surface)', color: 'var(--text)', font: 'inherit' }
const btn: React.CSSProperties = { minHeight: 40, padding: '0 14px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer' }

// NewTask: botão "Nova tarefa" que abre um formulário inline e cria o card no Backlog.
export function NewTask() {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [goal, setGoal] = useState('')
  const [project, setProject] = useState('')
  const [error, setError] = useState('')
  const qc = useQueryClient()
  const projects = useProjects().data
  const { projectId } = useProjectFilter()
  const chosen = project || String(projectId ?? projects?.[0]?.id ?? '')
  const create = useMutation({
    mutationFn: () =>
      post('/api/cards', { title: title.trim(), goal: goal.trim(), project_id: Number(chosen) }),
    onSuccess: () => {
      setOpen(false); setTitle(''); setGoal(''); setError('')
      qc.invalidateQueries({ queryKey: ['cards'] }); qc.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (e: Error) => setError(e.message),
  })

  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} style={{ ...btn, minHeight: 44, padding: '0 18px', border: 0, background: 'var(--text)', color: 'var(--bg)', fontWeight: 600 }}>
        Nova tarefa
      </button>
    )
  }
  if (projects && projects.length === 0) {
    return (
      <p style={{ margin: 0, fontSize: 13 }}>
        <Link to="/config">Cadastre um projeto em Configuração</Link>{' '}
        <button type="button" style={btn} onClick={() => setOpen(false)}>Fechar</button>
      </p>
    )
  }
  return (
    <form
      onSubmit={(e) => { e.preventDefault(); if (title.trim() && projects?.length) create.mutate() }}
      style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'flex-end' }}
    >
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, color: 'var(--text-2)' }}>
        Título
        <input value={title} onChange={(e) => setTitle(e.target.value)} style={field} />
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, color: 'var(--text-2)' }}>
        Objetivo (opcional)
        <input value={goal} onChange={(e) => setGoal(e.target.value)} style={field} />
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, color: 'var(--text-2)' }}>
        Projeto
        <select value={chosen} onChange={(e) => setProject(e.target.value)} style={field}>
          {(projects ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      </label>
      <button type="submit" style={{ ...btn, background: '#2A2F3A', color: '#fff' }} disabled={create.isPending || !title.trim()}>Criar tarefa</button>
      <button type="button" style={btn} onClick={() => { setOpen(false); setError('') }}>Cancelar</button>
      {error && <span role="alert" style={{ color: 'var(--err)', fontSize: 13, flexBasis: '100%' }}>{error}</span>}
    </form>
  )
}
