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
import { Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { searchUsers } from '@/features/users/api'
import { useDebounce } from '@/hooks'

import type { ChannelUserRateLimit } from '../types'

interface ChannelUserRateLimitEditorProps {
  value: ChannelUserRateLimit[]
  onChange: (value: ChannelUserRateLimit[]) => void
}

export function ChannelUserRateLimitEditor(
  props: ChannelUserRateLimitEditorProps
) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebounce(search, 300)
  const keyword = debouncedSearch.trim()

  const { data, isFetching } = useQuery({
    queryKey: ['channel-user-rate-limit-search', keyword],
    queryFn: () => searchUsers({ keyword, page_size: 20 }),
    enabled: open && keyword.length > 0,
    staleTime: 30_000,
  })

  const selectedIds = new Set(props.value.map((limit) => limit.user_id))
  const results = (data?.data?.items ?? []).filter(
    (user) =>
      !selectedIds.has(user.id) &&
      (user.DeletedAt === undefined || user.DeletedAt === null)
  )
  let emptyText = t('No users found')
  if (keyword.length === 0) {
    emptyText = t('Type a username, email, or ID')
  } else if (isFetching) {
    emptyText = t('Searching...')
  }

  const updateLimit = (
    userId: number,
    field: 'rpm_limit' | 'concurrency_limit',
    rawValue: string
  ) => {
    props.onChange(
      props.value.map((limit) =>
        limit.user_id === userId
          ? {
              ...limit,
              [field]: rawValue === '' ? undefined : Number(rawValue),
            }
          : limit
      )
    )
  }

  const updatePriority = (userId: number, rawValue: string) => {
    props.onChange(
      props.value.map((limit) =>
        limit.user_id === userId
          ? {
              ...limit,
              priority: rawValue === '' ? undefined : Number(rawValue),
            }
          : limit
      )
    )
  }

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Overrides apply only to this channel. Users without an override still use the channel-wide limits.'
          )}
        </p>
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            render={
              <Button type='button' variant='outline' size='sm'>
                <Plus className='mr-1 size-4' />
                {t('Add user')}
              </Button>
            }
          />
          <PopoverContent align='end' className='w-80 p-0'>
            <Command shouldFilter={false}>
              <CommandInput
                value={search}
                onValueChange={setSearch}
                placeholder={t('Search by username, email, or ID')}
              />
              <CommandList>
                <CommandEmpty>{emptyText}</CommandEmpty>
                {results.map((user) => (
                  <CommandItem
                    key={user.id}
                    value={String(user.id)}
                    onSelect={() => {
                      props.onChange([
                        ...props.value,
                        {
                          user_id: user.id,
                          username: user.display_name || user.username,
                        },
                      ])
                      setSearch('')
                      setOpen(false)
                    }}
                  >
                    <span className='truncate'>
                      {user.display_name || user.username}
                    </span>
                    <span className='text-muted-foreground ml-auto text-xs'>
                      #{user.id}
                    </span>
                  </CommandItem>
                ))}
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Higher priority values take channel concurrency slots first. Lower-priority requests use free slots but yield when a higher-priority request is waiting; active requests finish normally. All user, channel, and global limits still apply.'
        )}
      </p>

      {props.value.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t('No per-user overrides.')}
        </p>
      ) : (
        <div className='space-y-2'>
          {props.value.map((limit) => (
            <div
              key={limit.user_id}
              className='flex flex-wrap items-end gap-2 rounded-lg border p-3'
            >
              <div className='min-w-32 flex-1'>
                <Label className='text-xs'>
                  {limit.username || `#${limit.user_id}`}
                </Label>
                <p className='text-muted-foreground text-xs'>
                  #{limit.user_id}
                </p>
              </div>
              <div className='w-28'>
                <Label
                  htmlFor={`channel-user-rpm-${limit.user_id}`}
                  className='text-xs'
                >
                  {t('Requests per minute')}
                </Label>
                <Input
                  id={`channel-user-rpm-${limit.user_id}`}
                  type='number'
                  min={1}
                  max={2_147_483_647}
                  step={1}
                  value={limit.rpm_limit ?? ''}
                  placeholder={t('Unlimited')}
                  onChange={(event) =>
                    updateLimit(limit.user_id, 'rpm_limit', event.target.value)
                  }
                />
              </div>
              <div className='w-28'>
                <Label
                  htmlFor={`channel-user-concurrency-${limit.user_id}`}
                  className='text-xs'
                >
                  {t('Concurrent requests')}
                </Label>
                <Input
                  id={`channel-user-concurrency-${limit.user_id}`}
                  type='number'
                  min={1}
                  max={2_147_483_647}
                  step={1}
                  value={limit.concurrency_limit ?? ''}
                  placeholder={t('Unlimited')}
                  onChange={(event) =>
                    updateLimit(
                      limit.user_id,
                      'concurrency_limit',
                      event.target.value
                    )
                  }
                />
              </div>
              <div className='w-24'>
                <Label
                  htmlFor={`channel-user-priority-${limit.user_id}`}
                  className='text-xs'
                >
                  {t('Priority')}
                </Label>
                <Input
                  id={`channel-user-priority-${limit.user_id}`}
                  type='number'
                  step={1}
                  value={limit.priority ?? ''}
                  placeholder='0'
                  onChange={(event) =>
                    updatePriority(limit.user_id, event.target.value)
                  }
                />
              </div>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                aria-label={t('Remove')}
                onClick={() =>
                  props.onChange(
                    props.value.filter((item) => item.user_id !== limit.user_id)
                  )
                }
              >
                <Trash2 className='size-4' />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
