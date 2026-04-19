'use client'

import { useEffect, useRef, useState } from 'react'
import { api } from './api'
import type {
  Repo, Process, Contract, Caveat, Activity, Guard, Community,
  DashboardSnapshot,
} from './schema'

type Async<T> = {
  data: T | null
  loading: boolean
  error: string | null
  refetch: () => void
}

// useAsync runs an async fetcher on mount and exposes loading / error
// state. It re-runs whenever any item in `deps` changes, and `refetch`
// triggers a manual reload (used by reconnect / SSE invalidation).
function useAsync<T>(fetcher: () => Promise<T>, deps: unknown[] = []): Async<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const tick = useRef(0)

  const run = () => {
    const t = ++tick.current
    setLoading(true)
    setError(null)
    fetcher()
      .then((r) => {
        if (t !== tick.current) return
        setData(r)
        setError(null)
      })
      .catch((e: Error) => {
        if (t !== tick.current) return
        setError(e.message)
        setData(null)
      })
      .finally(() => {
        if (t !== tick.current) return
        setLoading(false)
      })
  }

  useEffect(() => {
    run()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, loading, error, refetch: run }
}

export function useDashboard(): Async<DashboardSnapshot> {
  return useAsync(() => api.dashboard())
}

export function useRepos(): Async<Repo[]> {
  return useAsync(async () => (await api.repos()).repos)
}

export function useProcesses(): Async<Process[]> {
  return useAsync(async () => (await api.processes()).processes)
}

export function useContracts(): Async<Contract[]> {
  return useAsync(async () => (await api.contracts()).contracts)
}

export function useCommunities(): Async<{ communities: Community[]; modularity: number }> {
  return useAsync(async () => api.communities())
}

export function useGuards(): Async<Guard[]> {
  return useAsync(async () => (await api.guards()).guards)
}

export function useCaveats(): Async<Caveat[]> {
  return useAsync(async () => (await api.caveats()).caveats)
}

export function useActivity(limit = 20): Async<Activity[]> {
  return useAsync(async () => (await api.activity(limit)).events, [limit])
}

export function useSymbolSearch(query: string, limit = 20) {
  return useAsync(() => api.searchSymbols(query, limit), [query, limit])
}

export function useUsages(id: string | null) {
  return useAsync(async () => (id ? api.findUsages(id) : Promise.resolve(null)), [id])
}

export function useDependencies(id: string | null) {
  return useAsync(async () => (id ? api.getDependencies(id) : Promise.resolve(null)), [id])
}
