import { useQuery } from '@tanstack/react-query'
import { get } from './client'
import type { Health, Settings } from './types'

export const useHealth = () =>
  useQuery({ queryKey: ['health'], queryFn: () => get<Health>('/api/health'), refetchInterval: 15000, retry: false })

export const useSettings = () =>
  useQuery({ queryKey: ['settings'], queryFn: () => get<Settings>('/api/settings') })
