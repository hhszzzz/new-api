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
import { useCallback, useEffect, useRef, useState } from 'react'

type LatestFormLoadOptions<TTarget, TValue> = {
  enabled: boolean
  target: TTarget | null
  load: (target: TTarget) => Promise<TValue>
  onLoad: (value: TValue, target: TTarget) => void
  onError?: (error: unknown, target: TTarget) => void
}

export function useLatestFormLoad<TTarget, TValue>(
  options: LatestFormLoadOptions<TTarget, TValue>
) {
  const requestIdRef = useRef(0)
  const scopeRef = useRef({ enabled: options.enabled, target: options.target })
  const loadRef = useRef(options.load)
  const onLoadRef = useRef(options.onLoad)
  const onErrorRef = useRef(options.onError)
  const [loadedTarget, setLoadedTarget] = useState<TTarget | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  scopeRef.current = { enabled: options.enabled, target: options.target }
  loadRef.current = options.load
  onLoadRef.current = options.onLoad
  onErrorRef.current = options.onError

  const reload = useCallback(async () => {
    const scope = scopeRef.current
    if (!scope.enabled || scope.target === null) return false

    const requestId = ++requestIdRef.current
    const target = scope.target
    setIsRefreshing(true)
    try {
      const value = await loadRef.current(target)
      const currentScope = scopeRef.current
      if (
        requestIdRef.current !== requestId ||
        !currentScope.enabled ||
        !Object.is(currentScope.target, target)
      ) {
        return false
      }
      onLoadRef.current(value, target)
      setLoadedTarget(target)
      return true
    } catch (error) {
      const currentScope = scopeRef.current
      if (
        requestIdRef.current === requestId &&
        currentScope.enabled &&
        Object.is(currentScope.target, target)
      ) {
        onErrorRef.current?.(error, target)
      }
      return false
    } finally {
      const currentScope = scopeRef.current
      if (
        requestIdRef.current === requestId &&
        currentScope.enabled &&
        Object.is(currentScope.target, target)
      ) {
        setIsRefreshing(false)
      }
    }
  }, [])

  useEffect(() => {
    if (!options.enabled || options.target === null) {
      requestIdRef.current += 1
      setLoadedTarget(null)
      setIsRefreshing(false)
      return
    }

    void reload()
    return () => {
      requestIdRef.current += 1
    }
  }, [options.enabled, options.target, reload])

  const isLoading =
    options.enabled &&
    options.target !== null &&
    (!Object.is(loadedTarget, options.target) || isRefreshing)

  return { isLoading, reload }
}
