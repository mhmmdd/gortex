'use client'

import { useMemo } from 'react'
import { useInspector } from '@/lib/inspector'
import type { Repo } from '@/lib/schema'
import { rnd, sampleSymbolName } from './rng'

type Node = { x: number; y: number; repo: string; color: string; size: number; label: string | null }

function buildConstellation(repos: Repo[]): Node[] {
  const r = rnd(7)
  const out: Node[] = []
  repos.forEach((rep) => {
    const cx = 200 + 700 * r()
    const cy = 160 + 420 * r()
    const n = Math.min(60, Math.max(10, Math.round(rep.nodes / 180)))
    for (let i = 0; i < n; i++) {
      const rr = Math.sqrt(r()) * (60 + 50 * (rep.nodes > 500 ? 1 : 0.5))
      const t = r() * Math.PI * 2
      const size = 1.5 + r() * (rep.nodes > 2000 ? 5 : 3.5)
      out.push({
        x: cx + Math.cos(t) * rr,
        y: cy + Math.sin(t) * rr,
        repo: rep.id,
        color: rep.color,
        size,
        label: i === 0 ? rep.id : r() > 0.85 ? sampleSymbolName(r) : null,
      })
    }
  })
  return out
}

export function GraphConstellation({ repos, filterRepos }: { repos: Repo[]; filterRepos: Set<string> }) {
  const setSym = useInspector((s) => s.setSym)
  const nodes = useMemo(() => buildConstellation(repos), [repos])
  const filt = useMemo(
    () => nodes.filter((n) => !filterRepos.size || filterRepos.has(n.repo)),
    [nodes, filterRepos],
  )
  const edges = useMemo(() => {
    const es: [number, number, number][] = []
    const r = rnd(11)
    for (let i = 0; i < 260; i++) {
      const a = Math.floor(r() * filt.length)
      const b = Math.floor(r() * filt.length)
      if (a !== b && filt[a] && filt[b]) {
        es.push([a, b, filt[a].repo === filt[b].repo ? 0.35 : 0.8])
      }
    }
    return es
  }, [filt])

  return (
    <svg viewBox="0 0 1100 640" width="100%" height="100%" style={{ display: 'block' }}>
      {edges.map(([a, b, w], i) => (
        <line
          key={i}
          x1={filt[a].x}
          y1={filt[a].y}
          x2={filt[b].x}
          y2={filt[b].y}
          stroke={w > 0.5 ? 'var(--accent)' : 'var(--line-2)'}
          strokeWidth={w > 0.5 ? 0.5 : 0.3}
          opacity={w > 0.5 ? 0.3 : 0.5}
        />
      ))}
      {filt.map((n, i) => (
        <g
          key={i}
          onClick={() =>
            n.label &&
            setSym({
              id: `${n.repo}::${n.label}`,
              kind: 'function',
              name: n.label,
              repo: n.repo,
              file: '',
              sig: '',
              callers: 0,
              callees: 0,
              community: '',
              caveats: [],
            })
          }
          style={{ cursor: n.label ? 'pointer' : 'default' }}
        >
          <circle cx={n.x} cy={n.y} r={n.size} fill={n.color} opacity="0.9" />
          {n.label && (
            <text x={n.x + n.size + 3} y={n.y + 3} fontFamily="JetBrains Mono" fontSize="10" fill="var(--fg-1)" opacity="0.9">
              {n.label}
            </text>
          )}
        </g>
      ))}
    </svg>
  )
}
