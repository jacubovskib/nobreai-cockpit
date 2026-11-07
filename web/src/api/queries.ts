import { useQuery } from '@tanstack/react-query'
import { get } from './client'
import type { CardSummary, Health, Overview, Settings } from './types'

export const useHealth = () =>
  useQuery({ queryKey: ['health'], queryFn: () => get<Health>('/api/health'), refetchInterval: 15000, retry: false })

export const useSettings = () =>
  useQuery({ queryKey: ['settings'], queryFn: () => get<Settings>('/api/settings') })

export const useOverview = () =>
  useQuery({ queryKey: ['overview'], queryFn: () => get<Overview>('/api/overview') })

export const useCards = () =>
  useQuery({ queryKey: ['cards'], queryFn: () => get<CardSummary[]>('/api/cards') })
