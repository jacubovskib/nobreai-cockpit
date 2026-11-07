import type { CSSProperties } from 'react'
import { ApiError } from '../../api/client'

export const panel: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, padding: 18, borderRadius: 12, background: 'var(--surface)', border: '1px solid var(--border)' }
export const btn: CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 40, padding: '0 14px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer', textDecoration: 'none' }
export const btnPrimary: CSSProperties = { ...btn, border: 0, background: '#2A2F3A', color: '#fff' }
export const h2: CSSProperties = { fontSize: 15, fontWeight: 600 }
export const input: CSSProperties = { minHeight: 40, padding: '0 10px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'var(--bg)', color: 'var(--text)', font: 'inherit' }
export const muted: CSSProperties = { fontSize: 13, color: 'var(--text-2)' }

export const errText = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : 'erro inesperado')

export function ErrorLine({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <p role="alert" style={{ margin: 0, fontSize: 13, color: 'var(--err)' }}>
      {errText(error)}
    </p>
  )
}
