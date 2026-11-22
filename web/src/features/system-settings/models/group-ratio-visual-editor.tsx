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
import { AlertTriangle, ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useCallback, memo, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { safeJsonParse } from '../utils/json-parser'
import { GroupRegistryEditor } from './group-registry-editor'
import { GroupSpecialUsableRulesEditor } from './group-special-usable-editor'

export type GroupSettingsSection = 'groups' | 'access' | 'overrides'

type GroupRatioVisualEditorProps = {
  section: GroupSettingsSection
  onSectionChange: (section: GroupSettingsSection) => void
  defaultUseAutoGroupField: ReactNode
  groupRatio: string
  groupGroupRatio: string
  groupSpecialUsableGroup: string
  onRegistryChanged: () => void
  onChange: (field: string, value: string) => void
}

type RegistryEntry = {
  name: string
  ratio: number
}

const sectionCardClassName = 'min-w-0 shadow-none'
const sectionHeaderClassName = 'gap-2 border-b'

function normalizeRatio(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 1
}

function parseRatioMap(value: string): Record<string, number> {
  return safeJsonParse<Record<string, number>>(value, {
    fallback: {},
    silent: true,
  })
}

function parseNestedRatioMap(
  value: string
): Record<string, Record<string, number>> {
  return safeJsonParse<Record<string, Record<string, number>>>(value, {
    fallback: {},
    silent: true,
  })
}

type GroupNameSelectProps = {
  options: string[]
  value: string | null
  placeholder: string
  onValueChange: (value: string) => void
  className?: string
}

function GroupNameSelect(props: GroupNameSelectProps) {
  const options = useMemo(() => {
    if (props.value && !props.options.includes(props.value)) {
      return [props.value, ...props.options]
    }
    return props.options
  }, [props.options, props.value])

  return (
    <Combobox
      options={options.map((name) => ({ value: name, label: name }))}
      value={props.value}
      onValueChange={(value) => {
        if (value) props.onValueChange(value)
      }}
      className={props.className ?? 'w-48'}
      placeholder={props.placeholder}
      aria-label={props.placeholder}
    />
  )
}

export const GroupRatioVisualEditor = memo(function GroupRatioVisualEditor({
  section,
  onSectionChange,
  defaultUseAutoGroupField,
  groupRatio,
  groupGroupRatio,
  groupSpecialUsableGroup,
  onRegistryChanged,
  onChange,
}: GroupRatioVisualEditorProps) {
  const { t } = useTranslation()

  const registry = useMemo<RegistryEntry[]>(() => {
    const ratioMap = parseRatioMap(groupRatio)
    return Object.keys(ratioMap).map((name) => ({
      name,
      ratio: normalizeRatio(ratioMap[name]),
    }))
  }, [groupRatio])

  const registryNames = useMemo(
    () => registry.map((entry) => entry.name),
    [registry]
  )

  return (
    <Tabs
      value={section}
      onValueChange={(value) => onSectionChange(value as GroupSettingsSection)}
      className='min-w-0 gap-5'
    >
      <div className='min-w-0 overflow-x-auto pb-1'>
        <TabsList aria-label={t('Group settings')} className='w-full min-w-max'>
          <TabsTrigger value='groups' className='px-3'>
            {t('Groups')}
          </TabsTrigger>
          <TabsTrigger value='access' className='px-3'>
            {t('Primary-group access')}
          </TabsTrigger>
          <TabsTrigger value='overrides' className='px-3'>
            {t('Billing overrides')}
          </TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value='groups' keepMounted>
        <Card className={sectionCardClassName}>
          <CardContent className='pt-4'>
            <GroupRegistryEditor onChanged={onRegistryChanged} />
            <div className='mt-5 border-t pt-4'>{defaultUseAutoGroupField}</div>
          </CardContent>
        </Card>
      </TabsContent>
      <TabsContent value='access' keepMounted>
        <GroupSpecialUsableRulesEditor
          value={groupSpecialUsableGroup}
          groupOptions={registryNames}
          onChange={(value) => onChange('GroupSpecialUsableGroup', value)}
        />
      </TabsContent>
      <TabsContent value='overrides' keepMounted>
        <GroupOverrideRules
          registry={registry}
          groupGroupRatio={groupGroupRatio}
          onChange={onChange}
        />
      </TabsContent>
    </Tabs>
  )
})

type GroupOverride = {
  targetGroup: string
  ratio: number
}

type GroupOverrideRulesProps = {
  registry: RegistryEntry[]
  groupGroupRatio: string
  onChange: (field: string, value: string) => void
}

function GroupOverrideRules({
  registry,
  groupGroupRatio,
  onChange,
}: GroupOverrideRulesProps) {
  const { t } = useTranslation()
  const [userGroupDialogOpen, setUserGroupDialogOpen] = useState(false)
  const [userGroupInput, setUserGroupInput] = useState<string | null>(null)
  const [overrideDialogOpen, setOverrideDialogOpen] = useState(false)
  const [overrideUserGroup, setOverrideUserGroup] = useState<string | null>(
    null
  )
  const [overrideEditData, setOverrideEditData] =
    useState<GroupOverride | null>(null)

  const registryNames = useMemo(
    () => registry.map((entry) => entry.name),
    [registry]
  )

  const baseRatioByName = useMemo(() => {
    const map = new Map<string, number>()
    for (const entry of registry) map.set(entry.name, entry.ratio)
    return map
  }, [registry])

  const groupGroupRatioList = useMemo(() => {
    const map = parseNestedRatioMap(groupGroupRatio)
    return Object.entries(map).map(([userGroup, overrides]) => ({
      userGroup,
      overrides: Object.entries(overrides).map(([targetGroup, ratio]) => ({
        targetGroup,
        ratio,
      })),
    }))
  }, [groupGroupRatio])

  const emitMap = useCallback(
    (map: Record<string, Record<string, number>>) => {
      onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
    },
    [onChange]
  )

  const handleUserGroupSave = useCallback(() => {
    if (!userGroupInput) return
    const map = parseNestedRatioMap(groupGroupRatio)
    if (!map[userGroupInput]) {
      map[userGroupInput] = {}
    }
    emitMap(map)
    setUserGroupDialogOpen(false)
    setUserGroupInput(null)
  }, [userGroupInput, groupGroupRatio, emitMap])

  const handleUserGroupDelete = useCallback(
    (userGroup: string) => {
      const map = parseNestedRatioMap(groupGroupRatio)
      delete map[userGroup]
      emitMap(map)
    },
    [groupGroupRatio, emitMap]
  )

  const handleOverrideAdd = useCallback((userGroup: string) => {
    setOverrideUserGroup(userGroup)
    setOverrideEditData(null)
    setOverrideDialogOpen(true)
  }, [])

  const handleOverrideEdit = useCallback(
    (userGroup: string, override: GroupOverride) => {
      setOverrideUserGroup(userGroup)
      setOverrideEditData(override)
      setOverrideDialogOpen(true)
    },
    []
  )

  const handleOverrideSave = useCallback(
    (targetGroup: string, ratio: number, oldTargetGroup?: string) => {
      if (!overrideUserGroup) return
      const map = parseNestedRatioMap(groupGroupRatio)
      if (!map[overrideUserGroup]) {
        map[overrideUserGroup] = {}
      }
      if (oldTargetGroup && oldTargetGroup !== targetGroup) {
        delete map[overrideUserGroup][oldTargetGroup]
      }
      map[overrideUserGroup][targetGroup] = ratio
      emitMap(map)
      setOverrideDialogOpen(false)
    },
    [overrideUserGroup, groupGroupRatio, emitMap]
  )

  const handleOverrideDelete = useCallback(
    (userGroup: string, targetGroup: string) => {
      const map = parseNestedRatioMap(groupGroupRatio)
      if (map[userGroup]) {
        delete map[userGroup][targetGroup]
        if (Object.keys(map[userGroup]).length === 0) {
          delete map[userGroup]
        }
      }
      emitMap(map)
    },
    [groupGroupRatio, emitMap]
  )

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Primary-group billing overrides')}</CardTitle>
        <CardDescription>
          {t(
            'Each rule sets the billing ratio for users with one primary group when their token uses a specific billing group.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='space-y-4'>
          <Button
            onClick={() => {
              setUserGroupInput(null)
              setUserGroupDialogOpen(true)
            }}
            size='sm'
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add primary group')}
          </Button>
          {groupGroupRatioList.length === 0 && (
            <EmptyState
              className='min-h-40'
              title={t('No special ratios configured')}
              description={t(
                'Base group ratios apply until you add an override.'
              )}
            />
          )}
          {groupGroupRatioList.length > 0 && (
            <div className='space-y-3'>
              {groupGroupRatioList.map((userGroupData) => (
                <Collapsible key={userGroupData.userGroup} defaultOpen>
                  <div className='rounded-lg border'>
                    <div className='flex items-center justify-between gap-2 p-3'>
                      <CollapsibleTrigger
                        render={
                          <Button
                            variant='ghost'
                            className='h-auto min-w-0 justify-start whitespace-normal'
                          />
                        }
                        aria-label={t('Rules for {{group}}', {
                          group: userGroupData.userGroup,
                        })}
                      >
                        <ChevronDown
                          className='size-4 shrink-0'
                          aria-hidden='true'
                        />
                        <span className='min-w-0 truncate font-semibold'>
                          {userGroupData.userGroup}
                        </span>
                        {!registryNames.includes(userGroupData.userGroup) && (
                          <AlertTriangle
                            className='text-destructive h-4 w-4'
                            aria-label={t('Group is not in the registry')}
                          />
                        )}
                        <span className='text-muted-foreground text-sm'>
                          {t('{{count}} override', {
                            count: userGroupData.overrides.length,
                          })}
                        </span>
                      </CollapsibleTrigger>
                      <div className='flex shrink-0 gap-1'>
                        <Button
                          variant='ghost'
                          size='sm'
                          aria-label={t('Add ratio override')}
                          onClick={() =>
                            handleOverrideAdd(userGroupData.userGroup)
                          }
                        >
                          <Plus className='h-4 w-4' />
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          aria-label={t('Remove {{group}}', {
                            group: userGroupData.userGroup,
                          })}
                          onClick={() =>
                            handleUserGroupDelete(userGroupData.userGroup)
                          }
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </div>
                    </div>
                    <CollapsibleContent>
                      {userGroupData.overrides.length > 0 && (
                        <div className='border-t'>
                          <StaticDataTable
                            className='rounded-none border-0'
                            data={userGroupData.overrides}
                            getRowKey={(override) => override.targetGroup}
                            columns={[
                              {
                                id: 'target-group',
                                header: t('Billing group'),
                                cellClassName: 'font-medium',
                                cell: (override) => (
                                  <span className='inline-flex max-w-48 items-center gap-1.5'>
                                    <span
                                      className='truncate'
                                      title={override.targetGroup}
                                    >
                                      {override.targetGroup}
                                    </span>
                                    {!registryNames.includes(
                                      override.targetGroup
                                    ) && (
                                      <AlertTriangle
                                        className='text-destructive h-3.5 w-3.5'
                                        aria-label={t(
                                          'Group is not in the registry'
                                        )}
                                      />
                                    )}
                                  </span>
                                ),
                              },
                              {
                                id: 'ratio',
                                header: t('Ratio'),
                                cell: (override) => {
                                  const baseRatio = baseRatioByName.get(
                                    override.targetGroup
                                  )
                                  return (
                                    <span className='inline-flex items-center gap-1.5'>
                                      {override.ratio}
                                      {baseRatio !== undefined &&
                                        baseRatio !== override.ratio && (
                                          <span className='text-muted-foreground text-xs'>
                                            {t('(instead of {{ratio}})', {
                                              ratio: baseRatio,
                                            })}
                                          </span>
                                        )}
                                    </span>
                                  )
                                },
                              },
                              {
                                id: 'actions',
                                header: t('Actions'),
                                className: 'text-right',
                                cellClassName: 'text-right',
                                cell: (override) => (
                                  <StaticRowActions
                                    editLabel={t('Edit')}
                                    deleteLabel={t('Delete')}
                                    menuLabel={t('Open menu')}
                                    onEdit={() =>
                                      handleOverrideEdit(
                                        userGroupData.userGroup,
                                        override
                                      )
                                    }
                                    onDelete={() =>
                                      handleOverrideDelete(
                                        userGroupData.userGroup,
                                        override.targetGroup
                                      )
                                    }
                                  />
                                ),
                              },
                            ]}
                          />
                        </div>
                      )}
                    </CollapsibleContent>
                  </div>
                </Collapsible>
              ))}
            </div>
          )}
        </div>
      </CardContent>

      {/* Add user group dialog */}
      <Dialog
        open={userGroupDialogOpen}
        onOpenChange={setUserGroupDialogOpen}
        title={t('Add primary group')}
        description={t(
          'Choose a primary group to configure its billing overrides.'
        )}
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              variant='outline'
              onClick={() => setUserGroupDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleUserGroupSave} disabled={!userGroupInput}>
              {t('Add')}
            </Button>
          </>
        }
      >
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>{t('Primary group')}</Label>
            <GroupNameSelect
              className='w-full'
              options={registryNames}
              value={userGroupInput}
              placeholder={t('Select a group')}
              onValueChange={setUserGroupInput}
            />
          </div>
        </div>
      </Dialog>

      <GroupOverrideDialog
        open={overrideDialogOpen}
        onOpenChange={setOverrideDialogOpen}
        onSave={handleOverrideSave}
        editData={overrideEditData}
        userGroup={overrideUserGroup}
        groupOptions={registryNames}
        baseRatioByName={baseRatioByName}
      />
    </Card>
  )
}

type GroupOverrideDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (targetGroup: string, ratio: number, oldTargetGroup?: string) => void
  editData: GroupOverride | null
  userGroup: string | null
  groupOptions: string[]
  baseRatioByName: Map<string, number>
}

function GroupOverrideDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  userGroup,
  groupOptions,
  baseRatioByName,
}: GroupOverrideDialogProps) {
  const { t } = useTranslation()
  const [targetGroup, setTargetGroup] = useState<string | null>(null)
  const [ratio, setRatio] = useState('')
  const [dialogScope, setDialogScope] = useState<
    { open: boolean; editData: GroupOverride | null } | undefined
  >(undefined)
  if (
    !dialogScope ||
    dialogScope.open !== open ||
    dialogScope.editData !== editData
  ) {
    setDialogScope({ open, editData })
    if (!open) {
      setTargetGroup(null)
      setRatio('')
    } else {
      setTargetGroup(editData?.targetGroup ?? null)
      setRatio(editData ? String(editData.ratio) : '')
    }
  }

  const baseRatio = targetGroup ? baseRatioByName.get(targetGroup) : undefined

  const handleSave = () => {
    if (!targetGroup || !ratio.trim()) return
    const parsedRatio = Number.parseFloat(ratio)
    if (Number.isNaN(parsedRatio)) return

    onSave(targetGroup, parsedRatio, editData?.targetGroup)
    setTargetGroup(null)
    setRatio('')
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={editData ? t('Edit ratio override') : t('Add ratio override')}
      description={
        userGroup
          ? t(
              'Set the billing ratio for users whose primary group is {{userGroup}} when their token uses a specific billing group.',
              { userGroup }
            )
          : t(
              'Set the billing ratio for each primary group and billing group pair.'
            )
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <div className='space-y-4 py-4'>
        <div className='space-y-2'>
          <Label>{t('Billing group')}</Label>
          <GroupNameSelect
            className='w-full'
            options={groupOptions}
            value={targetGroup}
            placeholder={t('Select a group')}
            onValueChange={setTargetGroup}
          />
          <p className='text-muted-foreground text-xs'>
            {t('The billing group used by the token')}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Ratio')}</Label>
          <Input
            value={ratio}
            onChange={(e) => {
              const val = e.target.value
              if (val === '' || !Number.isNaN(Number.parseFloat(val))) {
                setRatio(val)
              }
            }}
            placeholder={baseRatio === undefined ? '0.9' : String(baseRatio)}
          />
          <p className='text-muted-foreground text-xs'>
            {baseRatio !== undefined
              ? t('(instead of {{ratio}})', { ratio: baseRatio })
              : t(
                  'Users with primary group {{userGroup}} pay this ratio when billed as {{targetGroup}}.',
                  {
                    userGroup: userGroup || t('this primary group'),
                    targetGroup: targetGroup || t('this billing group'),
                  }
                )}
          </p>
        </div>
      </div>
    </Dialog>
  )
}
