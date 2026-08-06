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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import type { ChatEntryData } from './chat-config'

export type { ChatEntryData } from './chat-config'

const createChatDialogSchema = (t: (key: string) => string) =>
  z.object({
    name: z.string().min(1, t('Chat client name is required')),
    url: z.string().min(1, t('URL is required')),
    groups: z.array(z.string()),
  })

type ChatDialogFormValues = z.infer<ReturnType<typeof createChatDialogSchema>>

const CHAT_DIALOG_FORM_ID = 'chat-dialog-form'

type ChatDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ChatEntryData) => void
  editData?: ChatEntryData | null
  groupOptions: Option[]
}

export function ChatDialog(props: ChatDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!props.editData
  const chatDialogSchema = createChatDialogSchema(t)

  const form = useForm<ChatDialogFormValues>({
    resolver: zodResolver(chatDialogSchema),
    defaultValues: {
      name: '',
      url: '',
      groups: [],
    },
  })

  useEffect(() => {
    if (props.editData) {
      form.reset(props.editData)
    } else {
      form.reset({
        name: '',
        url: '',
        groups: [],
      })
    }
  }, [props.editData, form, props.open])

  const handleSubmit = (values: ChatDialogFormValues) => {
    props.onSave(values)
    form.reset()
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEditMode ? t('Edit chat preset') : t('Add chat preset')}
      description={t('Configure a predefined chat link for end users.')}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={CHAT_DIALOG_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={CHAT_DIALOG_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Chat Client Name')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('Please enter chat client name')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Display name for this chat client.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('URL')}</FormLabel>
                <FormControl>
                  <Input placeholder={t('Please enter the URL')} {...field} />
                </FormControl>
                <FormDescription>
                  {t('The URL for this chat client.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('User Groups')}</FormLabel>
                <FormControl>
                  <MultiSelect
                    id='chat-preset-groups'
                    options={props.groupOptions}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('Select groups')}
                    maxVisibleChips={5}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave empty to allow all user groups.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
