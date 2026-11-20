// Dados de teste compartilhados pelas telas de worktrees.
import type { WorktreeResources, WorktreeSession } from '../api/types'

export const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()
export const session = (o: Partial<WorktreeSession>): WorktreeSession => ({
  id: 'claude:x', agent: 'claude', provider_session_id: 'x', card_id: null, project_id: 1, pid: 1, cwd: '/w/api',
  branch: 'main', git_head: '', worktree: '/w/api', worktree_linked: false, model: 'claude-opus-5-5', transcript_path: '',
  full_capture: true, started_at: ago(12), last_event_at: ago(1), ended_at: null, end_reason: '', pid_dead_at: null, parent_session_id: '',
  live: true, last_activity: '', card: null, ...o,
})
export const res = (o: Partial<WorktreeResources>): WorktreeResources =>
  ({ ready: true, cpu_pct: 0, rss_bytes: 0, procs: 0, top: [], cpu_cores: 12, disk: null, ...o })
export const wt = {
  head: 'a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0', bare: false, detached: false, locked: false, prunable: false,
  lock_reason: '', prune_reason: '', resources: null,
}

