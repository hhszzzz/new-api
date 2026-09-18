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
import { toast } from 'sonner'
import { z } from 'zod'

import { Form, FormField, FormItem, FormMessage } from '@/components/ui/form'
import { parseProtocolPolicy } from '@/features/protocols/policy'
import { ProtocolPolicyEditor } from '@/features/protocols/protocol-policy-editor'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  policy: z
    .string()
    .refine(
      (value) => parseProtocolPolicy(value) !== null,
      'Invalid protocol policy JSON'
    ),
})

export function ProtocolSettingsCard(props: { value: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { policy: props.value },
  })
  useEffect(() => {
    form.reset({ policy: props.value })
  }, [props.value, form])

  async function onSubmit(values: z.infer<typeof schema>) {
    const value = JSON.stringify(parseProtocolPolicy(values.policy))
    if (value === JSON.stringify(parseProtocolPolicy(props.value))) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({ key: 'global.protocol_policy', value })
  }

  return (
    <SettingsSection title={t('Protocol settings')}>
      <p className='text-muted-foreground text-sm leading-relaxed'>
        {t(
          'Manage protocol conversion and conversation continuity in one place. Channels can override these settings individually.'
        )}
      </p>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='policy'
            render={({ field }) => (
              <FormItem data-settings-form-span='full'>
                <ProtocolPolicyEditor
                  value={field.value}
                  onChange={field.onChange}
                  disabled={updateOption.isPending}
                />
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
