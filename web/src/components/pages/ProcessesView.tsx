'use client'

import { useEffect, useState } from 'react'
import { Icon } from '@/components/primitives/Icon'
import { useProcesses, useRepos } from '@/lib/hooks'

export function ProcessesView() {
  const { data: processes, loading, error, refetch } = useProcesses()
  const { data: repos } = useRepos()
  const [sel, setSel] = useState<string | null>(null)

  useEffect(() => {
    if (!sel && processes && processes.length > 0) setSel(processes[0].id)
  }, [processes, sel])

  const repoColor = (id: string) => repos?.find((r) => r.id === id)?.color || 'var(--fg-2)'

  return (
    <>
      <div className="page-hd">
        <div>
          <h1>Processes</h1>
          <div className="sub">
            {loading
              ? 'Discovering execution flows…'
              : `${processes?.length ?? 0} flows discovered across ${
                  new Set(processes?.flatMap((p) => p.crosses) ?? []).size
                } repos`}
          </div>
        </div>
        <div className="actions">
          <button type="button" className="btn" onClick={refetch}>
            <Icon name="history" size={12} /> Refresh
          </button>
        </div>
      </div>

      {error && (
        <div style={{ padding: 22, color: 'var(--danger)', fontSize: 13 }}>
          Failed to load processes: {error}
        </div>
      )}

      {!error && (!processes || processes.length === 0) && !loading && (
        <div style={{ padding: 22, color: 'var(--fg-2)', fontSize: 13 }}>
          No processes discovered yet. Process detection runs after indexing — try re-indexing the repository.
        </div>
      )}

      {processes && processes.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: '1.5fr 1fr', flex: 1, minHeight: 0 }}>
          <div style={{ overflow: 'auto', borderRight: '1px solid var(--line-1)' }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th />
                  <th>Flow</th>
                  <th>Repos touched</th>
                  <th className="num">Steps</th>
                  <th className="num">Files</th>
                  <th className="num">Score</th>
                </tr>
              </thead>
              <tbody>
                {processes.map((p) => (
                  <tr
                    key={p.id}
                    onClick={() => setSel(p.id)}
                    className={sel === p.id ? 'active' : ''}
                    style={{ cursor: 'pointer' }}
                  >
                    <td style={{ width: 26, textAlign: 'center' }}>
                      <span
                        style={{
                          width: 6,
                          height: 6,
                          borderRadius: 50,
                          display: 'inline-block',
                          background:
                            p.risk === 'risk' ? 'var(--danger)' : p.risk === 'warn' ? 'var(--warn)' : 'var(--ok)',
                        }}
                      />
                    </td>
                    <td>
                      <div className="mono" style={{ color: 'var(--fg-0)' }}>{p.name}</div>
                      <div className="mono faint nowrap" style={{ fontSize: 10.5 }}>{p.entry}</div>
                    </td>
                    <td>
                      <div className="hstack" style={{ gap: 4, flexWrap: 'wrap' }}>
                        {p.crosses.map((r, i) => (
                          <span key={i} style={{ display: 'contents' }}>
                            {i > 0 && <span className="faint mono">→</span>}
                            <span className="tag-dim">{r}</span>
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="num">{p.steps}</td>
                    <td className="num">{p.files}</td>
                    <td className="num">{p.score}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ padding: 18, overflow: 'auto', background: 'var(--bg-1)' }}>
            {(() => {
              const p = processes.find((x) => x.id === sel) ?? processes[0]
              return (
                <div>
                  <div
                    className="mono faint"
                    style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.08em' }}
                  >
                    flow
                  </div>
                  <div style={{ fontSize: 18, fontWeight: 500, marginTop: 4 }}>{p.name}</div>
                  <div className="mono faint" style={{ fontSize: 11, marginTop: 4 }}>{p.entry}</div>

                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3,1fr)', gap: 8, marginTop: 14 }}>
                    <div className="card">
                      <div className="card-bd">
                        <div className="mono faint" style={{ fontSize: 10.5 }}>STEPS</div>
                        <div className="mono" style={{ fontSize: 22 }}>{p.steps}</div>
                      </div>
                    </div>
                    <div className="card">
                      <div className="card-bd">
                        <div className="mono faint" style={{ fontSize: 10.5 }}>FILES</div>
                        <div className="mono" style={{ fontSize: 22 }}>{p.files}</div>
                      </div>
                    </div>
                    <div className="card">
                      <div className="card-bd">
                        <div className="mono faint" style={{ fontSize: 10.5 }}>SCORE</div>
                        <div className="mono" style={{ fontSize: 22 }}>{p.score}</div>
                      </div>
                    </div>
                  </div>

                  <div
                    style={{
                      fontSize: 10.5,
                      textTransform: 'uppercase',
                      letterSpacing: '0.08em',
                      color: 'var(--fg-3)',
                      margin: '16px 0 8px',
                    }}
                  >
                    Repos on this flow
                  </div>
                  <div className="hstack" style={{ gap: 4, flexWrap: 'wrap' }}>
                    {p.crosses.map((r) => (
                      <span key={r} className="chip">
                        <span className="swatch" style={{ background: repoColor(r) }} />
                        {r}
                      </span>
                    ))}
                  </div>
                </div>
              )
            })()}
          </div>
        </div>
      )}
    </>
  )
}
