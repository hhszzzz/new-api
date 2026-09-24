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
import { useId, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

export function Sparkline(props: {
  values: number[]
  label: string
  windowHours?: 48 | 72
}) {
  const { t } = useTranslation()
  const gradientId = useId()
  const geometry = useMemo(() => {
    if (props.values.length < 2) return null
    const width = 180
    const height = 48
    const padding = 4
    const min = Math.min(...props.values)
    const max = Math.max(...props.values)
    const span = max - min || 1
    const stepX = (width - padding * 2) / (props.values.length - 1)
    const points = props.values.map((value, index) => {
      const x = padding + index * stepX
      const y = padding + (1 - (value - min) / span) * (height - padding * 2)
      return [x, y] as const
    })
    const line = points
      .map(
        ([x, y], index) =>
          `${index === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      )
      .join(' ')
    const area = `${line} L${(padding + (props.values.length - 1) * stepX).toFixed(1)},${height} L${padding},${height} Z`
    const last = points.at(-1)
    if (!last) return null
    return { width, height, line, area, last }
  }, [props.values])

  if (!geometry) {
    // A single reading exists but cannot form a trend yet.
    return (
      <div className='text-muted-foreground flex h-12 min-w-0 flex-1 items-center text-[11px]'>
        {props.values.length === 1
          ? t('Insufficient data')
          : t('No history data available')}
      </div>
    )
  }

  return (
    <svg
      viewBox={`0 0 ${geometry.width} ${geometry.height}`}
      className='h-12 min-w-0 flex-1'
      role='img'
      aria-label={
        props.windowHours === 72
          ? t('72-hour IQ trend for {{configuration}}', {
              configuration: props.label,
            })
          : t('48-hour IQ trend for {{configuration}}', {
              configuration: props.label,
            })
      }
      preserveAspectRatio='none'
    >
      <defs>
        <linearGradient id={gradientId} x1='0' y1='0' x2='0' y2='1'>
          <stop offset='0%' stopColor='var(--destructive)' stopOpacity='0.25' />
          <stop offset='100%' stopColor='var(--destructive)' stopOpacity='0' />
        </linearGradient>
      </defs>
      <path d={geometry.area} fill={`url(#${gradientId})`} />
      <path
        d={geometry.line}
        fill='none'
        stroke='var(--destructive)'
        strokeWidth='1.5'
        strokeLinejoin='round'
        strokeLinecap='round'
        vectorEffect='non-scaling-stroke'
      />
      <circle
        cx={geometry.last[0]}
        cy={geometry.last[1]}
        r='2.5'
        fill='var(--destructive)'
      />
    </svg>
  )
}
