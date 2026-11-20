import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from './client'
import type { AgentDetection, CardDetail, CardSummary, Limit, Price, Project, Prompt, Health, Overview, ProjectWorktrees, PromptEvent, WorktreeDetail, Settings, SessionWithUsage, Total, PlanLimits, ContextTop, ContextDetail, RtkStatus, RtkApplied, RtkSavings, Memory, MemoryPreview } from './types'

export const useHealth = () =>
  useQuery({ queryKey: ['health'], queryFn: () => get<Health>('/api/health'), refetchInterval: 15000, retry: false })

export const useSettings = () =>
  useQuery({ queryKey: ['settings'], queryFn: () => get<Settings>('/api/settings') })

// Os alertas do plano dependem do relógio (reinício da janela): relê a cada minuto.
export const useOverview = () =>
  useQuery({ queryKey: ['overview'], queryFn: () => get<Overview>('/api/overview'), refetchInterval: 60000 })

export const useCards = () =>
  useQuery({ queryKey: ['cards'], queryFn: () => get<CardSummary[]>('/api/cards') })

export const useProjects = () =>
  useQuery({ queryKey: ['projects'], queryFn: () => get<Project[]>('/api/projects') })

export const useCard = (key: string) =>
  useQuery({ queryKey: ['card', key], queryFn: () => get<CardDetail>(`/api/cards/${encodeURIComponent(key)}`), retry: false })

export const usePrompts = () =>
  useQuery({ queryKey: ['prompts'], queryFn: () => get<Prompt[]>('/api/prompts') })

export const useLimits = () =>
  useQuery({ queryKey: ['limits'], queryFn: () => get<Limit[]>('/api/limits') })

export const usePrices = () =>
  useQuery({ queryKey: ['prices'], queryFn: () => get<Price[]>('/api/prices') })

// group: agent | card | session | day_agent | model | project. from/to: AAAA-MM-DD locais, [from, to).
export const useTotals = (group: string, from?: string, to?: string, project?: number | null, exact = false) =>
  useQuery({
    queryKey: ['usage', 'totals', group, from ?? '', to ?? '', project ?? null, exact],
    queryFn: () => {
      const q = new URLSearchParams({ group })
      if (from) q.set('from', from)
      if (to) q.set('to', to)
      if (project != null) q.set('project', String(project))
      if (project != null && exact) q.set('exact', '1')
      return get<Total[]>(`/api/usage/totals?${q}`)
    },
  })

export const useSessions = (limit = 50) =>
  useQuery({ queryKey: ['sessions', limit], queryFn: () => get<SessionWithUsage[]>(`/api/sessions?limit=${limit}`) })

export const usePromptEvents = (limit = 50) =>
  useQuery({ queryKey: ['events', 'prompt', limit], queryFn: () => get<PromptEvent[]>(`/api/events?kind=prompt&limit=${limit}`) })

export const useAgents = () =>
  useQuery({ queryKey: ['agents'], queryFn: () => get<AgentDetection[]>('/api/agents') })

export const useWorktrees = () =>
  // CPU e memória mudam sem evento no stream: a página atualiza a cada 5 s
  // (o react-query só repete enquanto alguém observa a consulta).
  useQuery({ queryKey: ['worktrees'], queryFn: () => get<ProjectWorktrees[]>('/api/worktrees'), refetchInterval: 5000 })

export const useWorktreeDetail = (project: string, path: string) =>
  useQuery({
    queryKey: ['worktrees', 'detail', project, path],
    queryFn: () => get<WorktreeDetail>(`/api/worktrees/detail?${new URLSearchParams({ project, path })}`),
    refetchInterval: 5000,
    retry: false,
    enabled: !!project && !!path,
  })

// A hora relativa ("reinicia em…") muda sem evento: relê a cada minuto.
export const usePlanLimits = (hours = 24) =>
  useQuery({ queryKey: ['plan', hours], queryFn: () => get<PlanLimits>(`/api/plan-limits?hours=${hours}`), refetchInterval: 60000 })

export const useContextTop = (from?: string, to?: string, project?: number | null, exact = false) =>
  useQuery({
    queryKey: ['context', 'top', from ?? '', to ?? '', project ?? null, exact],
    queryFn: () => {
      const q = new URLSearchParams()
      if (from) q.set('from', from)
      if (to) q.set('to', to)
      if (project != null) q.set('project', String(project))
      if (project != null && exact) q.set('exact', '1')
      return get<ContextTop>(`/api/context/top?${q}`)
    },
  })

export const useContextSession = (id: string | null) =>
  useQuery({
    queryKey: ['context', 'session', id],
    queryFn: () => get<ContextDetail>(`/api/context/sessions/${encodeURIComponent(id!)}`),
    enabled: id !== null,
  })

export const useRtk = () =>
  useQuery({ queryKey: ['rtk'], queryFn: () => get<RtkStatus>('/api/rtk') })

export const useRtkPreview = (action: 'activate' | 'deactivate' | null) =>
  useQuery({
    queryKey: ['rtk', 'preview', action],
    queryFn: () => get<{ output: string }>(`/api/rtk/preview?action=${action}`),
    enabled: action !== null,
    gcTime: 0,
    retry: false,
  })

export function useRtkApply() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (action: 'activate' | 'deactivate') => post<RtkApplied>('/api/rtk', { action }),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: ['rtk'] }), qc.invalidateQueries({ queryKey: ['rtk-savings'] }), qc.invalidateQueries({ queryKey: ['overview'] }), qc.invalidateQueries({ queryKey: ['config-log'] })]),
  })
}

// Chave própria: o kind usage atualiza a economia sem refazer o estado do RTK
// (rtk --version) nem a prévia.
export const useRtkSavings = (from?: string, to?: string, project?: number | null, exact = false) =>
  useQuery({
    queryKey: ['rtk-savings', from ?? '', to ?? '', project ?? null, exact],
    queryFn: () => {
      const q = new URLSearchParams()
      if (from) q.set('from', from)
      if (to) q.set('to', to)
      if (project != null) q.set('project', String(project))
      if (project != null && exact) q.set('exact', '1')
      return get<RtkSavings>(`/api/rtk/savings?${q}`)
    },
  })

// family: numa raiz sem app escolhido, inclui as memórias dos apps (filhos).
export const useMemories = (projectId: number | null, family = false) =>
  useQuery({
    placeholderData: keepPreviousData,
    queryKey: ['memories', 'list', projectId, family],
    queryFn: () => get<Memory[]>(projectId === null ? '/api/memories' : `/api/memories?project=${projectId}${family ? '&family=1' : ''}`),
  })

export const useMemoryPreview = (projectId: number | null) =>
  useQuery({
    queryKey: ['memories', 'preview', projectId],
    enabled: projectId !== null,
    queryFn: () => get<MemoryPreview>(`/api/memories/preview?project=${projectId}`),
  })
