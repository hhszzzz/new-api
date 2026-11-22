import { Bot, Ban } from 'lucide-react'
import { type ReactNode, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import kimiCodeIcon from '@/assets/brand-icons/kimi-icon-round.png'
import {
  Avatar,
  AvatarFallback,
  AvatarGroup,
  AvatarGroupCount,
} from '@/components/ui/avatar'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { normalizeClientId } from '@/lib/client-policy'
import { getLobeIcon } from '@/lib/lobe-icon'

type ClientPolicy = {
  mode?: string
  clients?: string[]
}

const CLIENT_LABELS: Record<string, string> = {
  codex: 'Codex',
  claude_code: 'Claude Code',
  pi: 'Pi',
  deepseek_harness: 'DeepSeek Harness',
  antigravity: 'Antigravity',
  cline: 'Cline',
  opencode: 'OpenCode',
  kimi_code: 'Kimi Code',
  zcode: 'ZCode',
}

const CLIENT_ICONS: Record<string, string> = {
  codex: 'Codex.Color',
  claude_code: 'ClaudeCode.Color',
  pi: 'Pi',
  deepseek_harness: 'DeepSeek.Color',
  cline: 'Cline',
}

const CLIENT_ICON_IMAGES: Record<string, string> = {
  kimi_code: kimiCodeIcon,
}

function normalizeClients(clients: string[] | undefined): string[] {
  return [...new Set((clients ?? []).map(normalizeClientId).filter(Boolean))]
}

function clientLabel(client: string): string {
  return CLIENT_LABELS[client] ?? client
}

export function ClientPolicyIndicator(props: { policy?: ClientPolicy }) {
  const { t } = useTranslation()
  const clients = useMemo(
    () => normalizeClients(props.policy?.clients),
    [props.policy?.clients]
  )
  const mode = props.policy?.mode?.trim().toLowerCase()

  if (
    mode === 'unrestricted' ||
    !mode ||
    (mode !== 'allow' && mode !== 'deny')
  ) {
    return null
  }
  if (mode === 'deny' && clients.length === 0) {
    return null
  }

  const isAllowPolicy = mode === 'allow'
  const outlineColorClass = isAllowPolicy
    ? 'border-success'
    : 'border-destructive'
  const avatarOutlineClass = isAllowPolicy
    ? 'after:!border-success'
    : 'after:!border-destructive'
  const modeLabel = t(
    isAllowPolicy ? 'Allow only selected clients' : 'Block selected clients'
  )
  const clientLabels = clients.map(clientLabel)
  const description = `${modeLabel}: ${clientLabels.length > 0 ? clientLabels.join(', ') : t('None')}`

  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              aria-label={description}
              data-policy-mode={isAllowPolicy ? 'allow' : 'deny'}
              data-testid='client-policy-indicator'
              role='img'
              tabIndex={0}
              className='inline-flex shrink-0 cursor-help items-center'
            >
              {clients.length === 0 ? (
                <Avatar
                  size='sm'
                  className={`bg-background !size-5 ${avatarOutlineClass}`}
                >
                  <AvatarFallback className='bg-background text-destructive'>
                    <Ban className='size-3.5' aria-hidden='true' />
                  </AvatarFallback>
                </Avatar>
              ) : (
                <AvatarGroup className='-space-x-1.5'>
                  {clients.slice(0, 3).map((client) => {
                    const clientImage = CLIENT_ICON_IMAGES[client]
                    const clientIcon = CLIENT_ICONS[client]
                    let iconContent: ReactNode

                    if (clientImage) {
                      iconContent = (
                        <img
                          src={clientImage}
                          alt=''
                          aria-hidden='true'
                          className='size-full rounded-full object-contain'
                          data-testid={
                            client === 'kimi_code'
                              ? 'client-icon-kimi-code'
                              : undefined
                          }
                        />
                      )
                    } else if (clientIcon) {
                      iconContent = getLobeIcon(clientIcon, 14)
                    } else {
                      iconContent = (
                        <Bot
                          className='text-muted-foreground size-3.5'
                          aria-hidden='true'
                        />
                      )
                    }

                    return (
                      <Avatar
                        key={client}
                        size='sm'
                        className={`bg-background !size-5 ${avatarOutlineClass}`}
                      >
                        <AvatarFallback
                          className={`bg-background ${clientImage ? 'p-0' : 'p-0.5'}`}
                        >
                          {iconContent}
                        </AvatarFallback>
                      </Avatar>
                    )
                  })}
                  {clients.length > 3 && (
                    <AvatarGroupCount
                      className={`!size-5 border text-[9px] ${outlineColorClass}`}
                    >
                      +{clients.length - 3}
                    </AvatarGroupCount>
                  )}
                </AvatarGroup>
              )}
            </span>
          }
        />
        <TooltipContent side='top' className='max-w-sm'>
          {description}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
