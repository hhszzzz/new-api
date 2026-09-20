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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import { createPromptWordlist, updatePromptWordlist } from '../api'
import { PROMPT_AUDIT_SCOPES, promptAuditScopeLabel } from '../scopes'
import type {
  PromptAuditScope,
  PromptWordlist,
  PromptWordlistAction,
} from '../types'

interface WordlistImportValues {
  name: string
  source_url: string
  scopes: PromptAuditScope[]
  auto_update: boolean
  action: PromptWordlistAction
}

export function WordlistImportDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  wordlist?: PromptWordlist
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<WordlistImportValues>({
    resolver: zodResolver(
      z.object({
        name: z.string().trim().min(1, t('Enter a wordlist name')).max(128),
        source_url: z
          .string()
          .trim()
          .url({ error: t('Enter a valid HTTPS URL') })
          .refine(
            (value) => value.startsWith('https://'),
            t('Enter a valid HTTPS URL')
          ),
        scopes: z.array(z.enum(PROMPT_AUDIT_SCOPES)),
        auto_update: z.boolean(),
        action: z.enum(['block', 'review']),
      })
    ),
    defaultValues: {
      name: props.wordlist?.name ?? '',
      source_url: props.wordlist?.source_url ?? '',
      scopes: props.wordlist?.scopes ?? ['user', 'task'],
      auto_update: props.wordlist?.auto_update ?? true,
      action: props.wordlist?.action ?? 'review',
    },
  })
  const saveMutation = useMutation({
    mutationFn: (values: WordlistImportValues) =>
      props.wordlist
        ? updatePromptWordlist(props.wordlist.id, values)
        : createPromptWordlist(values),
    meta: { errorToast: false },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['prompt-audit'] })
      props.onOpenChange(false)
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  const editing = props.wordlist !== undefined
  let submitLabel = editing ? t('Save') : t('Import wordlist')
  if (saveMutation.isPending) {
    submitLabel = editing ? t('Saving...') : t('Importing...')
  }
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={props.wordlist ? t('Edit wordlist') : t('Import wordlist')}
      description={
        props.wordlist
          ? t(
              'Change the source or assignments. The previous successful version remains active until the updated source imports successfully.'
            )
          : t(
              'Paste a GitHub repository, directory, file, or HTTPS wordlist URL. Import runs on the server; entries are not shown here.'
            )
      }
      footer={
        <Button
          type='submit'
          form='wordlist-import-form'
          disabled={saveMutation.isPending}
        >
          {submitLabel}
        </Button>
      }
    >
      <Form {...form}>
        <form
          id='wordlist-import-form'
          onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='action'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Match action')}</FormLabel>
                <FormControl>
                  <NativeSelect {...field}>
                    <NativeSelectOption value='review'>
                      {t('Model confirmation')}
                    </NativeSelectOption>
                    <NativeSelectOption value='block'>
                      {t('Block immediately')}
                    </NativeSelectOption>
                  </NativeSelect>
                </FormControl>
                <p className='text-muted-foreground text-xs'>
                  {field.value === 'block'
                    ? t(
                        'A match is always blocked and cannot be overridden by model review.'
                      )
                    : t(
                        'A match is evidence for Qwen3Guard and optional gray-area review.'
                      )}
                </p>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Name')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={128} autoComplete='off' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='source_url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Source URL')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='url'
                    maxLength={2048}
                    placeholder='https://github.com/owner/wordlist'
                    autoComplete='off'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='scopes'
            render={({ field }) => (
              <FormItem>
                <FormLabel htmlFor='wordlist-import-scopes'>
                  {t('Apply to')}
                </FormLabel>
                <MultiSelect
                  id='wordlist-import-scopes'
                  aria-label={t('Apply to')}
                  options={PROMPT_AUDIT_SCOPES.map((scope) => ({
                    value: scope,
                    label: promptAuditScopeLabel(scope, t),
                  }))}
                  selected={field.value}
                  onChange={(values) => field.onChange(values)}
                />
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='auto_update'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <FormLabel>{t('Check for updates daily')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'A successful import becomes active for the selected sources. Failed updates keep the last successful version.'
            )}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Import limits: each file up to 10 MiB; all downloaded source data up to 30 MiB; up to 100 files and 500,000 unique words.'
            )}
          </p>
          {form.formState.errors.root && (
            <p role='alert' className='text-destructive text-sm'>
              {form.formState.errors.root.message}
            </p>
          )}
        </form>
      </Form>
    </Dialog>
  )
}
