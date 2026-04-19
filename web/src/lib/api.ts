import type {
  HealthResponse, ToolInfo, GraphStats, ToolResponse, GraphData,
  SubGraph, GortexNode, GraphChangeEvent, IndexHealth,
} from './types'
import type {
  Repo, Process, Contract, Caveat, Activity, Guard, Community,
  DashboardSnapshot, KindCount, LanguageCount,
} from './schema'

// Single base URL for the gortex server (http://.../v1/*).
const SERVER_URL = process.env.NEXT_PUBLIC_GORTEX_URL
  || process.env.NEXT_PUBLIC_GORTEX_WEB_URL
  || 'http://localhost:4747'

// Optional bearer token. Required when the server was started with
// --auth-token / $GORTEX_SERVER_TOKEN; otherwise leave unset.
const AUTH_TOKEN = process.env.NEXT_PUBLIC_GORTEX_TOKEN || ''

function authHeaders(): HeadersInit {
  return AUTH_TOKEN ? { Authorization: `Bearer ${AUTH_TOKEN}` } : {}
}

async function serverFetch(path: string, options?: RequestInit): Promise<Response> {
  const res = await fetch(`${SERVER_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...options?.headers,
    },
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`Server API error ${res.status}: ${text}`)
  }
  return res
}

async function callTool(name: string, args: Record<string, unknown> = {}): Promise<string> {
  const res = await serverFetch(`/v1/tools/${name}`, {
    method: 'POST',
    body: JSON.stringify({ arguments: args }),
  })
  const data: ToolResponse = await res.json()
  if (data.isError) {
    throw new Error(data.content?.[0]?.text || 'Tool call failed')
  }
  return data.content?.map(c => c.text).join('\n') || ''
}

async function callToolJSON<T>(name: string, args: Record<string, unknown> = {}): Promise<T> {
  const text = await callTool(name, args)
  try {
    return JSON.parse(text) as T
  } catch {
    return { nodes: [], edges: [], text } as unknown as T
  }
}

export const api = {
  // --- Health & stats ---
  health: async (): Promise<HealthResponse> => {
    const res = await serverFetch('/v1/health')
    return res.json()
  },

  tools: async (): Promise<ToolInfo[]> => {
    const res = await serverFetch('/v1/tools')
    return res.json()
  },

  stats: async (): Promise<GraphStats> => {
    const res = await serverFetch('/v1/stats')
    return res.json()
  },

  // --- Brief graph dump (force-directed rendering) ---
  getGraph: async (opts?: { project?: string; repo?: string }): Promise<GraphData> => {
    const qs = new URLSearchParams()
    if (opts?.project) qs.set('project', opts.project)
    if (opts?.repo) qs.set('repo', opts.repo)
    const suffix = qs.toString() ? `?${qs}` : ''
    const res = await serverFetch(`/v1/graph${suffix}`)
    return res.json()
  },

  // --- UI-shaped /v1 endpoints (added for the design) ---
  dashboard: async (): Promise<DashboardSnapshot> => {
    const res = await serverFetch('/v1/dashboard')
    return res.json()
  },

  repos: async (): Promise<{ repos: Repo[] }> => {
    const res = await serverFetch('/v1/repos')
    return res.json()
  },

  processes: async (): Promise<{ processes: Process[] }> => {
    const res = await serverFetch('/v1/processes')
    return res.json()
  },

  contracts: async (): Promise<{ contracts: Contract[] }> => {
    const res = await serverFetch('/v1/contracts')
    return res.json()
  },

  communities: async (): Promise<{ communities: Community[]; modularity: number }> => {
    const res = await serverFetch('/v1/communities')
    return res.json()
  },

  guards: async (): Promise<{ guards: Guard[] }> => {
    const res = await serverFetch('/v1/guards')
    return res.json()
  },

  caveats: async (): Promise<{ caveats: Caveat[] }> => {
    const res = await serverFetch('/v1/caveats')
    return res.json()
  },

  activity: async (limit = 50): Promise<{ events: Activity[] }> => {
    const res = await serverFetch(`/v1/activity?limit=${limit}`)
    return res.json()
  },

  // --- Symbol-level MCP tool wrappers ---
  searchSymbols: async (query: string, limit = 20): Promise<SymbolSearchResult[]> => {
    if (!query.trim()) return []
    const text = await callTool('search_symbols', { query, limit, format: 'json' })
    try {
      const parsed = JSON.parse(text) as { results?: SymbolSearchResult[] } | SymbolSearchResult[]
      if (Array.isArray(parsed)) return parsed
      return parsed.results ?? []
    } catch {
      return []
    }
  },

  getSymbol: async (id: string): Promise<GortexNode | null> => {
    try {
      return await callToolJSON<GortexNode>('get_symbol', { id })
    } catch { return null }
  },

  getSymbolSource: async (id: string): Promise<string> => {
    const result = await callTool('get_symbol_source', { id })
    try {
      const parsed = JSON.parse(result)
      return parsed.source || result
    } catch { return result }
  },

  getCallers: async (id: string, depth = 2): Promise<SubGraph> => {
    return callToolJSON<SubGraph>('get_callers', { id, depth })
  },

  getCallChain: async (id: string, depth = 2): Promise<SubGraph> => {
    return callToolJSON<SubGraph>('get_call_chain', { id, depth })
  },

  findUsages: async (id: string): Promise<SubGraph> => {
    return callToolJSON<SubGraph>('find_usages', { id })
  },

  getDependencies: async (id: string): Promise<SubGraph> => {
    return callToolJSON<SubGraph>('get_dependencies', { id })
  },

  getDependents: async (id: string): Promise<SubGraph> => {
    return callToolJSON<SubGraph>('get_dependents', { id })
  },

  indexHealth: async (): Promise<IndexHealth> => {
    return callToolJSON<IndexHealth>('index_health', {})
  },

  // --- Raw escape hatches (do not use in pages) ---
  callTool,
  callToolJSON,

  // --- SSE for live activity ---
  subscribeEvents: (callback: (event: GraphChangeEvent) => void): EventSource => {
    const qs = AUTH_TOKEN ? `?token=${encodeURIComponent(AUTH_TOKEN)}` : ''
    const es = new EventSource(`${SERVER_URL}/v1/events${qs}`)
    es.addEventListener('graph_change', (e) => {
      try {
        const data = JSON.parse(e.data) as GraphChangeEvent
        callback(data)
      } catch { /* ignore parse errors */ }
    })
    return es
  },
}

export type { Repo, Process, Contract, Caveat, Activity, Guard, Community, DashboardSnapshot, KindCount, LanguageCount }

export type SymbolSearchResult = {
  id: string
  kind: string
  name: string
  path: string
  line: number
  sig?: string
}
