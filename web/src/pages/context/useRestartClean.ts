import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { post } from '../../api/client'

// O servidor recusa um "Onde parei" mais velho que isto.
export const WHERE_STOPPED_MAX_MIN = 30

export interface RestartCleanOptions {
  sessionId: string
  whereStopped: string
  whereStoppedAt: string | null
  mode?: 'background' // ausente = o corpo do POST não leva mode
  now?: number
  onDone?: (terminalId: string | undefined) => void
}

export interface RestartClean {
  text: string
  setText(v: string): void
  needsText: boolean // registro velho/vazio, ou o servidor pediu ("registre onde parou")
  canSend: boolean
  pending: boolean
  done: boolean
  terminalId: string | undefined
  error: Error | null
  send(): void
}

// Lógica do "recomeçar limpo": só envia o texto se foi editado ou se o registro
// está velho/vazio. Compartilhada pelo painel do card e pelo tile da grade.
export function useRestartClean({ sessionId, whereStopped, whereStoppedAt, mode, now = Date.now(), onDone }: RestartCleanOptions): RestartClean {
  const qc = useQueryClient()
  const [text, setText] = useState(whereStopped)
  const [serverAsked, setServerAsked] = useState(false)
  const old = !whereStoppedAt || now - Date.parse(whereStoppedAt) > WHERE_STOPPED_MAX_MIN * 60000
  const edited = text.trim() !== whereStopped.trim()
  const needsText = old || serverAsked
  const m = useMutation({
    mutationFn: () => {
      const body: { where_stopped?: string; mode?: 'background' } = {}
      if (edited || needsText) body.where_stopped = text.trim()
      if (mode) body.mode = mode
      return post<{ terminal_id?: string }>(`/api/sessions/${encodeURIComponent(sessionId)}/restart-clean`, body)
    },
    onSuccess: (r) => onDone?.(r?.terminal_id),
    onError: (e: Error) => { if (/registre onde parou/.test(e.message)) setServerAsked(true) },
    onSettled: () => Promise.all(['card', 'cards', 'sessions', 'context-live', 'session-context', 'terminals', 'overview'].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  })
  return {
    text, setText, needsText,
    canSend: !m.isPending && !(needsText && text.trim() === ''),
    pending: m.isPending, done: m.isSuccess,
    terminalId: m.data?.terminal_id,
    error: m.error,
    send: () => m.mutate(),
  }
}
