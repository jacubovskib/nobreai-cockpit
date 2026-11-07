import { Link } from 'react-router-dom'

// WorktreeTag mostra o worktree git de uma sessão (nome curto, caminho
// completo no title) e leva à página de worktrees. Sem worktree, nada.
export function WorktreeTag({ path, linked, branch }: { path: string; linked: boolean; branch?: string }) {
  if (!path) return null
  const name = path.split('/').filter(Boolean).pop() ?? path
  return (
    <Link
      to="/worktrees"
      title={path}
      aria-label={`Worktree ${name}${linked ? '' : ' (principal)'}${branch ? `, branch ${branch}` : ''}`}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12.5, color: 'var(--text-2)', textDecoration: 'none' }}
    >
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
        <circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="8" r="2" /><path d="M6 7v10M18 10c0 4-6 3-12 7" />
      </svg>
      <span className="mono" style={{ color: 'var(--text)' }}>{name}</span>
      {!linked && <span>principal</span>}
      {branch && <span className="mono">· {branch}</span>}
    </Link>
  )
}
