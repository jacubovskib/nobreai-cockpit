import { useState, type CSSProperties } from 'react'
import { useQuery } from '@tanstack/react-query'
import { get } from '../../api/client'
import type { FsDirs } from '../../api/types'
import { ErrorLine, btn, btnPrimary, input, muted } from '../card/shared'

const part: CSSProperties = { ...btn, minHeight: 32, padding: '0 8px', border: 0 }
const badge: CSSProperties = { marginLeft: 6 }

// Partes clicáveis do caminho: cada uma leva ao seu prefixo.
function crumbs(path: string): { label: string; path: string }[] {
  const names = path.split('/').filter(Boolean)
  return [{ label: '/', path: '/' }, ...names.map((n, i) => ({ label: n, path: '/' + names.slice(0, i + 1).join('/') }))]
}

// Seletor de pastas: só nomes de subpastas, dentro da home (o daemon recusa o resto).
export function FolderPicker({ start, onUse, onCancel }: { start?: string; onUse: (path: string) => void; onCancel: () => void }) {
  const [path, setPath] = useState(start ?? '')
  const [paste, setPaste] = useState('')
  const q = useQuery({
    queryKey: ['fs-dirs', path],
    queryFn: () => get<FsDirs>(path === '' ? '/api/fs/dirs' : `/api/fs/dirs?path=${encodeURIComponent(path)}`),
    retry: false,
  })
  const here = q.data?.path
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <nav aria-label="Caminho da pasta" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 2 }}>
        {here && crumbs(here).map((c, i) => (
          <span key={c.path} style={{ display: 'inline-flex', alignItems: 'center' }}>
            {i > 1 && <span style={muted} aria-hidden="true">/</span>}
            <button type="button" style={part} onClick={() => setPath(c.path)}>{c.label}</button>
          </span>
        ))}
      </nav>
      <form onSubmit={(e) => { e.preventDefault(); if (paste.trim() !== '') setPath(paste.trim()) }} style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)', flex: 1, minWidth: 240 }}>Colar caminho
          <input style={{ ...input, fontFamily: 'var(--mono)' }} placeholder="/home/você/workspace/projeto" value={paste} onChange={(e) => setPaste(e.target.value)} />
        </label>
        <button type="submit" style={{ ...btn, alignSelf: 'flex-end' }} disabled={paste.trim() === ''}>Ir</button>
      </form>
      {q.isPending && <p style={muted}>Lendo a pasta…</p>}
      <ErrorLine error={q.error} />
      {q.data && q.data.dirs.length === 0 && <p style={muted}>Nenhuma subpasta.</p>}
      {q.data && q.data.dirs.length > 0 && (
        <ul aria-label="Subpastas" style={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 4 }}>
          {q.data.parent !== '' && <li><button type="button" style={{ ...btn, width: '100%' }} onClick={() => setPath(q.data.parent)}>.. (pasta acima)</button></li>}
          {q.data.dirs.map((d) => (
            <li key={d.path}>
              <button type="button" style={{ ...btn, width: '100%', justifyContent: 'flex-start' }} onClick={() => setPath(d.path)}>
                {d.name}/
                {d.git && <span className="badge" style={badge}>git</span>}
                {d.registered && <span className="badge" style={badge}>cadastrado</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" style={btnPrimary} disabled={!here} onClick={() => here && onUse(here)}>Usar esta pasta</button>
        <button type="button" style={btn} onClick={onCancel}>Cancelar</button>
      </div>
    </div>
  )
}
