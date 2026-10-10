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

import { importPromptAuditArchives } from '../api'
import type { PromptAuditImportJob } from '../types'

export function ArchiveImportDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImported: (job: PromptAuditImportJob) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<{ files: File[] }>({
    resolver: zodResolver(
      z.object({
        files: z
          .array(z.custom<File>((value) => value instanceof File))
          .min(1, t('Select archive files'))
          .refine(
            (files) =>
              files.reduce((total, file) => total + file.size, 0) <=
              2 * 1024 ** 3,
            t('Archive files must fit within the 2 GiB import limit.')
          ),
      })
    ),
    defaultValues: { files: [] },
  })
  const upload = useMutation({
    mutationFn: async ({ files }: { files: File[] }) => {
      if (files.every((file) => file.name.endsWith('.tar.zst'))) {
        for (const file of files) await importPromptAuditArchives([file])
        return { status: 'done' } as PromptAuditImportJob
      }
      return importPromptAuditArchives(files)
    },
    meta: { errorToast: false },
    onSuccess: async (job) => {
      props.onImported(job)
      await client.invalidateQueries({ queryKey: ['prompt-audit'] })
      props.onOpenChange(false)
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import day packages')}
      description={t(
        'Select the downloaded volume manifest and every encrypted volume. Multiple days can be imported together.'
      )}
      footer={
        <Button
          type='submit'
          form='prompt-audit-archive-import'
          disabled={upload.isPending}
        >
          {upload.isPending ? t('Importing...') : t('Import')}
        </Button>
      }
    >
      <Form {...form}>
        <form
          id='prompt-audit-archive-import'
          className='space-y-4'
          onSubmit={form.handleSubmit((values) => upload.mutate(values))}
        >
          <FormField
            control={form.control}
            name='files'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Archive files')}</FormLabel>
                <FormControl>
                  <Input
                    type='file'
                    multiple
                    disabled={upload.isPending}
                    onChange={(event) =>
                      field.onChange([...(event.target.files ?? [])])
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Imports are a temporary viewing cache: 24 hours, up to 2 GiB. Original audit results are preserved.'
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
