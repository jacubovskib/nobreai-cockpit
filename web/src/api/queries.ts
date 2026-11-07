import { useQuery } from '@tanstack/react-query'
import { get } from './client'
import type { CardDetail, CardSummary, Limit, Price, Project, Prompt, Health, Overview, PromptEvent, Settings, SessionWithUsage, Total } from './types'

export const useHealth = () =>
  useQuery({ queryKey: ['health'], queryFn: () => get<Health>('/api/health'), refetchInterval: 15000, retry: false })

export const useSettings = () =>
  useQuery({ queryKey: ['settings'], queryFn: () => get<Settings>('/api/settings') })

export const useOverview = () =>
  useQuery({ queryKey: ['overview'], queryFn: () => get<Overview>('/api/overview') })

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

// group: agent | card | session | day_agent | model. from/to: AAAA-MM-DD locais, [from, to).
export const useTotals = (group: string, from?: string, to?: string) =>
  useQuery({
    queryKey: ['usage', 'totals', group, from ?? '', to ?? ''],
    queryFn: () => {
      const q = new URLSearchParams({ group })
      if (from) q.set('from', from)
      if (to) q.set('to', to)
      return get<Total[]>(`/api/usage/totals?${q}`)
    },
  })

export const useSessions = (limit = 50) =>
  useQuery({ queryKey: ['sessions', limit], queryFn: () => get<SessionWithUsage[]>(`/api/sessions?limit=${limit}`) })

export const usePromptEvents = (limit = 50) =>
  useQuery({ queryKey: ['events', 'prompt', limit], queryFn: () => get<PromptEvent[]>(`/api/events?kind=prompt&limit=${limit}`) })
