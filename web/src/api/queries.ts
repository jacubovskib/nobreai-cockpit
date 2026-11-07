import { useQuery } from '@tanstack/react-query'
import { get } from './client'
import type { CardDetail, CardSummary, Limit, Project, Prompt, Health, Overview, Settings } from './types'

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
