import type { Project } from '../api/types'
import { displayName, projectColor } from '../app/project'

// Etiqueta do projeto: sempre com o nome (a cor sozinha não basta).
export function ProjectTag({ project, projects }: { project: Project | null; projects: Project[] }) {
  const color = project ? projectColor(project.id, projects) : 'var(--text-2)'
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12.5, color: project ? 'var(--text)' : 'var(--text-2)' }}>
      <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="4" fill={color} /></svg>
      {project ? displayName(project, projects) : 'Sem projeto'}
    </span>
  )
}
