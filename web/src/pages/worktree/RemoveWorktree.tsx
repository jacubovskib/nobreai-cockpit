import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, del } from '../../api/client'
import { useWithProject } from '../../app/project'
import type { RemoveNeedsConfirm, RemoveWorktreeResult, WorktreeDetail } from '../../api/types'
import { btn, ErrorLine, muted } from '../card/shared'
import { mono } from '../worktreeUI'

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

  function close() {
    setOpen(false)
    setConfirm(null)
    setError(null)
  }

  async function send(force: boolean) {
    setBusy(true)
    setError(null)
    try {
      const r = await del<RemoveWorktreeResult>('/api/worktrees', { project: d.project.id, path: w.path, force, delete_branch: hasBranch && alsoBranch })
      qc.invalidateQueries({ queryKey: ['worktrees'] })
      const notice = r.branch_kept_reason ? `Branch ${w.branch} mantido: ${r.branch_kept_reason}.` : ''
      navigate(withP('/worktrees'), { state: notice ? { notice } : undefined })
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
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
              <input type="checkbox" checked={alsoBranch} disabled={busy} onChange={(e) => setAlsoBranch(e.target.checked)} />
              Também apagar o branch <span style={mono}>{w.branch}</span>
            </label>
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
