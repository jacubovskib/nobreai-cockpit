import { useSearchParams } from 'react-router-dom'
import { useProjects } from '../api/queries'
import type { Project } from '../api/types'

const PARAM = 'projeto'
// Paleta fixa por posição (ordem de id), em ciclo. Nunca as cores dos agentes.
export const PROJECT_COLORS = ['#B07CE8', '#C9A227', '#E26D9C', '#4FB3BF', '#9DB255', '#C38D6B']

export function projectColor(projectId: number, projects: Project[]): string {
  const sorted = [...projects].sort((a, b) => a.id - b.id)
  const i = sorted.findIndex((p) => p.id === projectId)
  return PROJECT_COLORS[(i < 0 ? 0 : i) % PROJECT_COLORS.length]
}

// isGroup: grupo organiza sub-projetos e não é um repositório (pode ou não ter pasta).
export function isGroup(p: Project): boolean {
  return p.kind === 'group'
}

// familyIds: o projeto e, se for pai, os filhos. Um filho é só ele mesmo.
export function familyIds(projects: Project[], id: number): number[] {
  return [id, ...projects.filter((p) => p.parent_id === id).map((p) => p.id)]
}

// displayName: "loja › web" para um filho, o nome simples para os demais.
export function displayName(p: Project, projects: Project[]): string {
  const parent = p.parent_id == null ? undefined : projects.find((x) => x.id === p.parent_id)
  return parent ? `${parent.name} › ${p.name}` : p.name
}

export function withProject(to: string, projectId: number | null): string {
  if (projectId === null) return to
  return `${to}${to.includes('?') ? '&' : '?'}${PARAM}=${projectId}`
}

// Mesma regra de store.ProjectForPath: prefixo mais longo, respeitando o limite de diretório.
export function projectForPath(projects: Project[], dir: string): Project | null {
  let best: Project | null = null
  for (const p of projects) {
    if (p.path === '') continue
    const root = p.path.replace(/\/+$/, '')
    if (dir !== root && !dir.startsWith(root + '/')) continue
    if (best === null || p.path.length > best.path.length) best = p
  }
  return best
}

export function useProjectFilter(): { projectId: number | null; project: Project | null; ids: number[] | null; setProjectId(id: number | null): void } {
  const [params, setParams] = useSearchParams()
  const projects = useProjects()
  const raw = params.get(PARAM)
  const id = raw !== null && /^\d+$/.test(raw) ? Number(raw) : null
  const project = id === null ? null : (projects.data ?? []).find((p) => p.id === id) ?? null
  return {
    projectId: project ? project.id : null,
    project,
    ids: project ? familyIds(projects.data ?? [], project.id) : null,
    setProjectId(next) {
      setParams((prev) => {
        const q = new URLSearchParams(prev)
        if (next === null) q.delete(PARAM)
        else q.set(PARAM, String(next))
        return q
      }, { replace: true })
    },
  }
}

// Aplica o projeto escolhido (?projeto=) a um link interno, para o filtro acompanhar a navegação.
export function useWithProject(): (to: string) => string {
  const { projectId } = useProjectFilter()
  return (to) => withProject(to, projectId)
}
