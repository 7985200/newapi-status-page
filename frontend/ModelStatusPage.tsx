/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Eye } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Dialog } from '@/components/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'

// Types

interface StatusChannel {
  id: number
  name: string
  display_name: string
  status: number
  response_time: number
  test_time: number
  test_error: string
}

interface StatusModel {
  model: string
  group: string
  total: number
  enabled: number
  status: string
  channels?: StatusChannel[]
  bars?: TimelineBar[]
}

interface StatusGroup {
  group: string
  status: string
  models: StatusModel[]
}

interface TimelineBar {
  ts: number
  status: string
  errors: number
  total: number
}

interface StatusPayload {
  updated_at: number
  groups: StatusGroup[]
  is_admin: boolean
  bars: TimelineBar[]
  uptime_pct: number
  auto_refresh: boolean
  auto_refresh_interval: number
}

// Colors

const BAR_COLOR: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded: 'bg-amber-500',
  outage: 'bg-red-500',
  none: 'bg-slate-300 dark:bg-slate-700',
}

const DOT_COLOR: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded: 'bg-amber-500',
  outage: 'bg-red-500',
  none: 'bg-slate-400',
}

const TEXT_COLOR: Record<string, string> = {
  operational: 'text-emerald-600 dark:text-emerald-400',
  degraded: 'text-amber-600 dark:text-amber-400',
  outage: 'text-red-600 dark:text-red-400',
  none: 'text-slate-500',
}

const STATUS_LABEL: Record<string, string> = {
  operational: '运行正常',
  degraded: '部分降级',
  outage: '不可用',
  none: '暂无数据',
}

function timeAgo(unix: number): string {
  if (!unix) return '未测试'
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - unix)
  if (diff < 60) return `${diff}秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  return `${Math.floor(diff / 86400)}天前`
}

// API

async function fetchModelStatus(): Promise<StatusPayload | undefined> {
  const res = await api.get('/api/model-status')
  return res.data?.data as StatusPayload | undefined
}

// Timeline bars

function Timeline({ bars }: { bars: TimelineBar[] }) {
  return (
    <div className='flex items-end gap-[2px]' style={{ height: '32px' }}>
      {bars.map((bar) => (
        <div
          key={bar.ts}
          title={`${new Date(bar.ts * 1000).toLocaleString()}\n${STATUS_LABEL[bar.status] || ''}${bar.errors > 0 ? ` (${bar.errors}次错误)` : ''}${bar.total > 0 ? ` · ${bar.total}次请求` : ''}`}
          className={`flex-1 rounded-[2px] transition-all hover:scale-y-110 hover:opacity-80 ${BAR_COLOR[bar.status] ?? BAR_COLOR.none}`}
          style={{ height: bar.status === 'none' ? '40%' : '100%' }}
        />
      ))}
    </div>
  )
}

// Page

export function ModelStatusPage() {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<string | null>(null)
  const [errorDialog, setErrorDialog] = useState<{ open: boolean; title: string; content: string }>({ open: false, title: '', content: '' })
  const [refreshSec, setRefreshSec] = useState(30)

  const query = useQuery({
    queryKey: ['model-status'],
    queryFn: fetchModelStatus,
    refetchInterval: refreshSec * 1000,
  })

  // 从首次响应里读取自动刷新设置
  const payload = query.data
  if (payload && payload.auto_refresh !== undefined) {
    const next = payload.auto_refresh ? payload.auto_refresh_interval : false
    const nextSec = typeof next === 'number' ? next : 30
    if (nextSec !== refreshSec && next !== false) setRefreshSec(nextSec)
    if (payload.auto_refresh === false && refreshSec !== 0) setRefreshSec(0)
  }

  const bars = payload?.bars ?? []
  const groups = payload?.groups ?? []
  const uptime = payload?.uptime_pct ?? 100

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[900px] space-y-4 px-3 pt-12 pb-10 sm:px-6 sm:pt-16 sm:pb-12'>
        {/* ======== Title bar ======== */}
        <div className='flex items-center justify-between'>
          <h1 className='text-base font-semibold tracking-tight sm:text-lg'>
            {t('Service Status')}
          </h1>
          <div className='flex items-center gap-3'>
            {payload?.updated_at ? (
              <span className='text-muted-foreground text-xs'>
                {new Date(payload.updated_at * 1000).toLocaleTimeString()}
              </span>
            ) : null}
            <button
              type='button'
              className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs'
              onClick={() => void query.refetch()}
            >
              <RefreshCw
                className={`h-3 w-3 ${query.isFetching ? 'animate-spin' : ''}`}
              />
            </button>
          </div>
        </div>

        {/* ======== Overall uptime timeline ======== */}
        <div className='rounded-lg border p-4'>
          <div className='mb-3 flex items-baseline justify-between'>
            <span className='text-sm font-medium'>
              {t('60-hour uptime')}
            </span>
            <span
              className={`text-2xl font-bold tabular-nums ${
                uptime >= 99
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : uptime >= 90
                    ? 'text-amber-600 dark:text-amber-400'
                    : 'text-red-600 dark:text-red-400'
              }`}
            >
              {uptime.toFixed(1)}%
            </span>
          </div>
          {query.isLoading ? (
            <Skeleton className='h-8 w-full' />
          ) : (
            <Timeline bars={bars} />
          )}
          <div className='text-muted-foreground mt-2 flex justify-between text-[11px] tabular-nums'>
            <span>{bars.length > 0 ? new Date(bars[0].ts * 1000).toLocaleDateString() : ''}</span>
            <span>{bars.length > 0 ? new Date(bars[bars.length - 1].ts * 1000).toLocaleDateString() : ''}</span>
          </div>
        </div>

        {/* ======== Groups list ======== */}
        {query.isLoading ? (
          <div className='space-y-3'>
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className='h-16 w-full rounded-lg' />
            ))}
          </div>
        ) : !payload ? (
          <div className='rounded-lg border px-6 py-8 text-center'>
            <p className='text-muted-foreground text-sm'>
              {t('Unable to load status data')}
            </p>
          </div>
        ) : groups.length === 0 ? (
          <div className='rounded-lg border px-6 py-8 text-center'>
            <p className='text-muted-foreground text-sm'>
              {t('No groups available yet')}
            </p>
          </div>
        ) : (
          <div className='space-y-2'>
            {groups.map((g) => {
              const isOpen = expanded === g.group
              const gOk = g.models.filter(
                (m) => m.status === 'operational'
              ).length
              const statusText = STATUS_LABEL[g.status] ?? ''
              return (
                <div key={g.group} className='rounded-lg border'>
                  {/* --- group row --- */}
                  <button
                    type='button'
                    className='hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors'
                    onClick={() => setExpanded(isOpen ? null : g.group)}
                  >
                    <span
                      className={`h-2.5 w-2.5 shrink-0 rounded-full ${DOT_COLOR[g.status] ?? DOT_COLOR.none}`}
                    />
                    <div className='min-w-0 flex-1'>
                      <span className='text-sm font-medium'>
                        {g.group === 'default' ? '默认分组' : g.group}
                      </span>
                    </div>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {gOk}/{g.models.length}
                    </span>
                    <span
                      className={`text-xs font-medium ${TEXT_COLOR[g.status] ?? ''}`}
                    >
                      {statusText}
                    </span>
                  </button>

                  {/* --- models detail --- */}
                  {isOpen && (
                    <div className='border-t'>
                      <ul className='divide-y'>
                        {g.models.map((m) => {
                          const mBars = m.bars ?? []
                          // 计算该模型可用率
                          let mOp = 0
                          for (const b of mBars) {
                            if (b.status === 'operational' || b.status === 'degraded') mOp++
                          }
                          const mUptime = mBars.length > 0 ? (mOp / mBars.length) * 100 : 100
                          return (
                            <li
                              key={`${g.group}-${m.model}`}
                              className='hover:bg-muted/30 px-4 py-2.5 transition-colors'
                            >
                              <div className='flex items-center gap-3'>
                                <span
                                  className={`h-2 w-2 shrink-0 rounded-full ${DOT_COLOR[m.status] ?? DOT_COLOR.none}`}
                                />
                                <span className='min-w-0 flex-1 truncate font-mono text-[13px]'>
                                  {m.model}
                                </span>
                                {/* 模型自己的时间轴 */}
                                {mBars.length > 0 && (
                                  <div className='hidden flex-1 items-center gap-[2px] sm:flex' style={{ height: '20px' }}>
                                    {mBars.map((bar) => (
                                      <div
                                        key={bar.ts}
                                        title={`${new Date(bar.ts * 1000).toLocaleString()}\n${STATUS_LABEL[bar.status] ?? ''}${bar.errors > 0 ? ` (${bar.errors}次错误)` : ''}`}
                                        className={`flex-1 rounded-[2px] ${BAR_COLOR[bar.status] ?? BAR_COLOR.none}`}
                                        style={{ height: bar.status === 'none' ? '40%' : '100%' }}
                                      />
                                    ))}
                                  </div>
                                )}
                                <span
                                  className={`w-12 shrink-0 text-right text-xs font-semibold tabular-nums ${
                                    mUptime >= 99
                                      ? 'text-emerald-600 dark:text-emerald-400'
                                      : mUptime >= 90
                                        ? 'text-amber-600 dark:text-amber-400'
                                        : 'text-red-600 dark:text-red-400'
                                  }`}
                                >
                                  {mUptime.toFixed(0)}%
                                </span>
                                <span
                                  className={`w-14 shrink-0 text-right text-xs font-medium ${TEXT_COLOR[m.status] ?? ''}`}
                                >
                                  {STATUS_LABEL[m.status] ?? ''}
                                </span>
                              </div>

                              {/* 管理员渠道明细表格 */}
                              {payload.is_admin &&
                                m.channels &&
                                m.channels.length > 0 && (
                                  <div className='mt-1.5 overflow-hidden rounded border'>
                                    <table className='w-full text-xs'>
                                      <thead className='bg-muted/50 text-muted-foreground'>
                                        <tr>
                                          <th className='px-2 py-1 text-left font-medium'>
                                            渠道
                                          </th>
                                          <th className='px-2 py-1 text-left font-medium'>
                                            状态
                                          </th>
                                          <th className='px-2 py-1 text-right font-medium'>
                                            耗时
                                          </th>
                                          <th className='px-2 py-1 text-right font-medium'>
                                            最近测试
                                          </th>
                                          <th className='px-2 py-1 text-center font-medium'>
                                            错误
                                          </th>
                                        </tr>
                                      </thead>
                                      <tbody>
                                        {m.channels.map((ch) => (
                                          <tr key={ch.id} className='border-t'>
                                            <td className='px-2 py-1 font-mono'>
                                              #{ch.id}
                                              {ch.display_name ? ` ${ch.display_name}` : ''}
                                            </td>
                                            <td
                                              className={`px-2 py-1 font-medium ${ch.status === 1 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}`}
                                            >
                                              {ch.status === 1
                                                ? '正常'
                                                : '禁用'}
                                            </td>
                                            <td className='text-muted-foreground px-2 py-1 text-right tabular-nums'>
                                              {ch.response_time > 0
                                                ? `${ch.response_time}ms`
                                                : '—'}
                                            </td>
                                            <td className='text-muted-foreground px-2 py-1 text-right'>
                                              {timeAgo(ch.test_time)}
                                            </td>
                                            <td className='px-2 py-1 text-center'>
                                              {ch.test_error ? (
                                                <button
                                                  type='button'
                                                  className='text-primary hover:underline inline-flex items-center gap-0.5 text-xs'
                                                  onClick={() =>
                                                    setErrorDialog({
                                                      open: true,
                                                      title: `#${ch.id}${ch.display_name ? ` ${ch.display_name}` : ''} 测试错误`,
                                                      content: ch.test_error,
                                                    })
                                                  }
                                                >
                                                  <Eye className='h-3 w-3' />
                                                  查看
                                                </button>
                                              ) : (
                                                <span className='text-muted-foreground/50'>—</span>
                                              )}
                                            </td>
                                          </tr>
                                        ))}
                                      </tbody>
                                    </table>
                                  </div>
                                )}
                            </li>
                          )
                        })}
                      </ul>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}

        <p className='text-muted-foreground/60 text-center text-[11px]'>
          {t('Status is computed from channel availability and refreshes every 60s')}
        </p>

        {/* 错误详情弹窗 */}
        <Dialog
          open={errorDialog.open}
          onOpenChange={(open) => setErrorDialog({ ...errorDialog, open })}
          title={errorDialog.title}
          contentClassName='sm:max-w-lg'
        >
          <pre className='bg-muted/50 max-h-[300px] overflow-auto whitespace-pre-wrap rounded-md border p-3 text-xs'>
            {errorDialog.content}
          </pre>
        </Dialog>
      </PageTransition>
    </PublicLayout>
  )
}
