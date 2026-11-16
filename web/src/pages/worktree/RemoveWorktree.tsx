import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, del } from '../../api/client'
import { useWithProject } from '../../app/project'
import type { RemoveNeedsConfirm, RemoveWorktreeResult, WorktreeDetail } from '../../api/types'
import { btn, ErrorLine, muted } from '../card/shared'
import { mono } from '../worktreeUI'
import { plural } from '../board'

const btnDanger: React.CSSProperties = { ...btn, borderColor: '#6b2c2c', color: 'var(--err)' }
const MAX_FILES = 10

function asNeedsConfirm(e: unknown): RemoveNeedsConfirm | null {
  if (!(e instanceof ApiError) || e.status !== 409) return null
  const b = e.body as Partial<RemoveNeedsConfirm> | null
  return b?.needs_confirm ? (b as RemoveNeedsConfirm) : null
}

// Excluir worktree: confirmação inline (sem diálogo do navegador). O servidor
// decide; um 409 needs_confirm lista o que se perde e libera "Excluir mesmo assim".
export function RemoveWorktree({ d }: { d: WorktreeDetail }) {
  const w = d.worktree
  const navigate = useNavigate()
  const qc = useQueryClient()
  const withP = useWithProject()
  const [open, setOpen] = useState(false)
  const [alsoBranch, setAlsoBranch] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [confirm, setConfirm] = useState<RemoveNeedsConfirm | null>(null)

  if (w.main || w.bare) return null
  const hasLive = d.sessions.some((s) => s.live)
  const reason = hasLive ? 'Há uma sessão viva neste worktree.' : w.locked ? 'Worktree travado.' : ''
  const hasBranch = !!w.branch && !w.detached
  // Commits fora da base: o servidor manteria o branch; a caixa nem libera.
  const ahead = d.git?.compared && d.git.ahead > 0 ? d.git.ahead : 0
  const deleteBranch = hasBranch && alsoBranch && ahead === 0

  function close() {
    setOpen(false)
    setConfirm(null)
    setError(null)
  }

  async function send(force: boolean) {
    setBusy(true)
    setError(null)
    try {
      const r = await del<RemoveWorktreeResult>('/api/worktrees', { project: d.project.id, path: w.path, force, delete_branch: deleteBranch })
      // O detalhe apagado sai do cache antes de invalidar a lista: sem isso ele
      // seria buscado de novo e piscaria "Worktree não encontrado".
      qc.removeQueries({ queryKey: ['worktrees', 'detail'] })
      const notice = r.branch_kept_reason ? `Branch ${w.branch} mantido: ${r.branch_kept_reason}.` : ''
      navigate(withP('/worktrees'), { replace: true, state: notice ? { notice } : undefined })
      qc.invalidateQueries({ queryKey: ['worktrees'] })
    } catch (e) {
      const nc = asNeedsConfirm(e)
      if (nc) setConfirm(nc)
      else setError(e)
      setBusy(false)
    }
  }

  const files = confirm?.files ?? []
  const procs = confirm?.processes ?? []
  const extra = confirm ? Math.max(confirm.total_files, files.length) - Math.min(files.length, MAX_FILES) : 0

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, alignItems: 'flex-start' }}>
      {!open && (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
          <button type="button" style={{ ...btnDanger, opacity: reason ? 0.5 : 1, cursor: reason ? 'not-allowed' : 'pointer' }} disabled={!!reason} onClick={() => setOpen(true)}>Excluir worktree</button>
          {reason && <span style={muted}>{reason}</span>}
        </div>
      )}
      {open && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10, alignItems: 'flex-start' }}>
          <span style={{ fontSize: 13 }}>Excluir a pasta <span style={mono}>{w.path}</span>?</span>
          <span style={muted}>Arquivos ignorados pelo git (como .env e node_modules) também serão apagados.</span>
          {hasBranch && (
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, opacity: ahead ? 0.6 : 1 }}>
                <input type="checkbox" checked={deleteBranch} disabled={busy || ahead > 0} onChange={(e) => setAlsoBranch(e.target.checked)} />
                Também apagar o branch <span style={mono}>{w.branch}</span>
              </label>
              {ahead > 0 && <span style={muted}>({plural(ahead, 'commit', 'commits')} fora de {d.git?.base}; o branch será mantido)</span>}
            </div>
          )}
          {confirm && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
              <span style={{ color: 'var(--warn)' }}>
                {files.length === 0 && procs.length === 0 ? confirm.error : 'O worktree tem alterações não commitadas ou processos rodando:'}
              </span>
              {files.length > 0 && (
                <ul aria-label="Arquivos com alterações" style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 2 }}>
                  {files.slice(0, MAX_FILES).map((f) => <li key={f.path} style={{ ...mono, fontSize: 12.5, overflowWrap: 'anywhere' }}>{f.code.trim() || f.code} {f.path}</li>)}
                  {extra > 0 && <li style={muted}>+{extra}</li>}
                </ul>
              )}
              {procs.length > 0 && (
                <ul aria-label="Processos rodando" style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 2 }}>
                  {procs.map((p) => <li key={p.pid} style={{ ...mono, fontSize: 12.5 }}>{p.name} (pid {p.pid})</li>)}
                </ul>
              )}
            </div>
          )}
          <ErrorLine error={error} />
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            <button type="button" style={btnDanger} disabled={busy} onClick={() => send(!!confirm)}>{confirm ? 'Excluir mesmo assim' : 'Sim, excluir'}</button>
            <button type="button" style={btn} disabled={busy} onClick={close}>Cancelar</button>
          </div>
        </div>
      )}
    </div>
  )
}
