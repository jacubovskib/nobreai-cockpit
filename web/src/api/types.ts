// Espelha os JSON da API do nobred (snake_case, como o Go emite).
// Datas chegam como string RFC3339.

export type Column = 'backlog' | 'em_andamento' | 'pausado' | 'revisao' | 'concluido'

export interface Health { ok: boolean; version: string }

export interface Project { id: number; name: string; path: string; capture: boolean }

export interface Card {
  id: number
  key: string
  title: string
  goal: string
  done_when: string
  where_stopped: string
  where_stopped_at: string | null
  how_to_test: string
  branch: string
  project_id: number
  column: Column
  created_at: string
  updated_at: string
}

export interface Session {
  id: string
  agent: string
  provider_session_id: string
  card_id: number | null
  project_id: number | null
  pid: number
  cwd: string
  branch: string
  git_head: string
  model: string
  transcript_path: string
  full_capture: boolean
  started_at: string
  last_event_at: string
  ended_at: string | null
  end_reason: string
  pid_dead_at: string | null
}

export interface DomainEvent {
  id: number
  session_id: string
  card_id: number | null
  kind: string
  data: unknown
  at: string
}

// GET /api/events?kind=prompt
export interface PromptEvent extends DomainEvent { agent: string; card_key: string }

export interface Decision { id: number; card_id: number; what: string; why: string; session_id: string; at: string }
export interface EssentialFile { id: number; card_id: number; path: string; lines: string; why: string }
export interface Todo { id: number; card_id: number; text: string; done: boolean; session_id: string }
export interface Question { id: number; card_id: number; text: string; answer: string; session_id: string }

export interface Content {
  decisions: Decision[]
  files: EssentialFile[]
  todos: Todo[]
  questions: Question[]
}

export interface Prompt {
  id: number
  name: string
  slug: string
  tag: string
  description: string
  body: string
  agents: string[]
  project_id: number | null
  is_default_handoff: boolean
  uses: number
  last_used_at: string | null
}

export interface Price {
  model: string
  input_per_m: number
  output_per_m: number
  cache_write_per_m: number
  cache_read_per_m: number
}

export interface Limit {
  agent: string
  daily_usd: number
  card_usd: number
  warn_pct: number
  action: 'pause' | 'warn'
}

export interface Settings {
  rules: Record<string, boolean>
  where_stopped_ttl_min: number
  grace_min: number
  reminder_edit_threshold: number
  launch_mode: string
  terminal_cmd: string[]
  currency: 'USD' | 'BRL' | string
  brl_rate: number
  incomplete_action: 'ask_agent' | 'mark' | string
  demo: boolean
}

export interface ReadinessItem { rule: string; label: string; ok: boolean }
export interface ReadinessResult { items: ReadinessItem[]; met: number; total: number; ready: boolean }

export interface Total {
  key: string
  agent: string
  day: string
  sessions: number
  input: number
  output: number
  cache_write: number
  cache_read: number
  cost_usd: number
  unpriced: boolean
}

export interface SessionWithUsage extends Session { usage: Total; unpriced: boolean }

export interface TouchedFile { path: string; kind: 'editado' | 'lido' | string; count: number; last_at: string }

// GET /api/cards (item do board)
export interface CardSummary extends Card {
  agent: string
  session_count: number
  live: boolean
  last_activity_at: string | null
  totals: Total
  readiness: ReadinessResult
}

// GET /api/cards/{key}
export interface CardDetail extends Card {
  content: Content
  sessions: SessionWithUsage[]
  readiness: ReadinessResult
  totals: Total
  live: Session | null
  touched_files: TouchedFile[]
}

export interface ActiveSession extends Session {
  card: Card | null
  last_activity: string
  session_usage: Total
  card_totals: Total
  readiness: ReadinessResult | null
}

export interface AgentSpend {
  agent: string
  spent_usd: number
  daily_usd: number
  pct: number
  warn_pct: number
  unpriced: boolean
}

export interface TodayTotals {
  active_agents: number
  cards_in_progress: number
  cards_without_live_session: number
  cost_usd: number
  cost_unpriced: boolean
  daily_limit_usd: number
  tokens_input: number
  tokens_output: number
  tokens_cache_write: number
  tokens_cache_read: number
  per_agent: AgentSpend[]
}

export interface Alert {
  kind: 'stale' | 'limit_warn' | 'incomplete' | 'unpriced' | string
  card_key: string
  agent: string
  text: string
}

// GET /api/overview
export interface Overview {
  active: ActiveSession[]
  today: TodayTotals
  alerts: Alert[]
  no_card: SessionWithUsage[]
  events: DomainEvent[]
}

// Evento SSE `changed`: data = {"kind": "..."}
export type StreamKind = 'cards' | 'sessions' | 'config' | 'usage' | 'prompts' | string
