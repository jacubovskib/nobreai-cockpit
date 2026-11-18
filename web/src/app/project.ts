import { useSearchParams } from 'react-router-dom'
import { useProjects } from '../api/queries'
import type { Project } from '../api/types'

const PARAM = 'projeto'
const APP_PARAM = 'app'
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

export type App = number | 'geral' | null

export function withProject(to: string, projectId: number | null, app?: App): string {
  if (projectId === null) return to
  const sep = to.includes('?') ? '&' : '?'
  const extra = app != null ? `&${APP_PARAM}=${app}` : ''
  return `${to}${sep}${PARAM}=${projectId}${extra}`
}

// effectiveIds: o filtro efetivo. Todos (app nulo) é a família, geral só o principal, um app só ele.
export function effectiveIds(projects: Project[], root: Project, app: App): number[] {
  if (app === 'geral') return [root.id]
  if (app !== null) return [app]
  return familyIds(projects, root.id)
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

export function useProjectFilter(): {
  projectId: number | null
  project: Project | null
  root: Project | null
  app: App
  ids: number[] | null
  setProjectId(id: number | null): void
  setApp(app: App): void
} {
  const [params, setParams] = useSearchParams()
  const projects = useProjects()
  const list = projects.data ?? []
  const raw = params.get(PARAM)
  const id = raw !== null && /^\d+$/.test(raw) ? Number(raw) : null
  const found = id === null ? null : list.find((p) => p.id === id) ?? null
  // Link antigo com o id de um filho vale "pai + app".
  const parent = found?.parent_id == null ? null : list.find((p) => p.id === found.parent_id) ?? null
  const root = parent ?? found
  const children = root ? list.filter((p) => p.parent_id === root.id) : []
  const rawApp = params.get(APP_PARAM)
  let app: App = null
  if (parent && found) app = found.id
  else if (rawApp === 'geral' && children.length > 0) app = 'geral'
  else if (rawApp !== null && /^\d+$/.test(rawApp) && children.some((c) => c.id === Number(rawApp))) app = Number(rawApp)
  return {
    projectId: root ? (typeof app === 'number' ? app : root.id) : null,
    project: root ? (typeof app === 'number' ? list.find((p) => p.id === app) ?? root : root) : null,
    root,
    app,
    ids: root ? effectiveIds(list, root, app) : null,
    setProjectId(next) {
      setParams((prev) => {
        const q = new URLSearchParams(prev)
        q.delete(APP_PARAM)
        if (next === null) q.delete(PARAM)
        else q.set(PARAM, String(next))
        return q
      }, { replace: true })
    },
    setApp(next) {
      setParams((prev) => {
        const q = new URLSearchParams(prev)
        if (root) q.set(PARAM, String(root.id))
        if (next === null) q.delete(APP_PARAM)
        else q.set(APP_PARAM, String(next))
        return q
      }, { replace: true })
    },
  }
}

// Aplica o projeto e o app escolhidos (?projeto=&app=) a um link interno, para o filtro acompanhar a navegação.
export function useWithProject(): (to: string) => string {
  const { root, app } = useProjectFilter()
  return (to) => withProject(to, root ? root.id : null, app)
}
