import type { CSSProperties } from 'react'
import { ApiError } from '../../api/client'

export const panel: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, padding: 18, borderRadius: 12, background: 'var(--surface)', border: '1px solid var(--border)' }
// Botão desabilitado: a regra button:disabled de styles/tokens.css (opacidade e cursor).
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

export const th: CSSProperties = { padding: '8px 12px', fontWeight: 500 }
export const td: CSSProperties = { padding: '10px 12px' }
export const thead: CSSProperties = { textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }
export const num: CSSProperties = { ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }
