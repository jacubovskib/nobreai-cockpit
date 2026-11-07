import { agentInfo } from './agents'

export function AgentSwatch({ agent }: { agent: string }) {
  const { name, color } = agentInfo(agent)
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span
        aria-hidden="true"
        style={{ width: 10, height: 10, borderRadius: 3, background: color, display: 'inline-block' }}
      />
      {name}
    </span>
  )
}
