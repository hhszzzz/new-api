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
/* eslint-disable react-refresh/only-export-components */
/**
 * LobeHub Icon Loader
 * Render common @lobehub/icons entries directly and lazily load uncommon
 * configured entries on demand.
 *
 * Supports:
 * - Basic: "OpenAI", "OpenAI.Color"
 * - Chained properties: "OpenAI.Avatar.type={'platform'}"
 * - Size parameter: getLobeIcon("OpenAI", 20)
 */
import Ai360Color from '@lobehub/icons/es/Ai360/components/Color.js'
import Ai360Mono from '@lobehub/icons/es/Ai360/components/Mono.js'
import * as Ai360Style from '@lobehub/icons/es/Ai360/style.js'
import AnthropicMono from '@lobehub/icons/es/Anthropic/components/Mono.js'
import * as AnthropicStyle from '@lobehub/icons/es/Anthropic/style.js'
import AwsColor from '@lobehub/icons/es/Aws/components/Color.js'
import AwsMono from '@lobehub/icons/es/Aws/components/Mono.js'
import * as AwsStyle from '@lobehub/icons/es/Aws/style.js'
import AzureColor from '@lobehub/icons/es/Azure/components/Color.js'
import AzureMono from '@lobehub/icons/es/Azure/components/Mono.js'
import * as AzureStyle from '@lobehub/icons/es/Azure/style.js'
import AzureAIColor from '@lobehub/icons/es/AzureAI/components/Color.js'
import AzureAIMono from '@lobehub/icons/es/AzureAI/components/Mono.js'
import * as AzureAIStyle from '@lobehub/icons/es/AzureAI/style.js'
import BaiduColor from '@lobehub/icons/es/Baidu/components/Color.js'
import BaiduMono from '@lobehub/icons/es/Baidu/components/Mono.js'
import * as BaiduStyle from '@lobehub/icons/es/Baidu/style.js'
import ClaudeColor from '@lobehub/icons/es/Claude/components/Color.js'
import ClaudeMono from '@lobehub/icons/es/Claude/components/Mono.js'
import * as ClaudeStyle from '@lobehub/icons/es/Claude/style.js'
import ClineMono from '@lobehub/icons/es/Cline/components/Mono.js'
import * as ClineStyle from '@lobehub/icons/es/Cline/style.js'
import CloudflareColor from '@lobehub/icons/es/Cloudflare/components/Color.js'
import CloudflareMono from '@lobehub/icons/es/Cloudflare/components/Mono.js'
import * as CloudflareStyle from '@lobehub/icons/es/Cloudflare/style.js'
import CohereColor from '@lobehub/icons/es/Cohere/components/Color.js'
import CohereMono from '@lobehub/icons/es/Cohere/components/Mono.js'
import * as CohereStyle from '@lobehub/icons/es/Cohere/style.js'
import CozeMono from '@lobehub/icons/es/Coze/components/Mono.js'
import * as CozeStyle from '@lobehub/icons/es/Coze/style.js'
import DeepSeekColor from '@lobehub/icons/es/DeepSeek/components/Color.js'
import DeepSeekMono from '@lobehub/icons/es/DeepSeek/components/Mono.js'
import * as DeepSeekStyle from '@lobehub/icons/es/DeepSeek/style.js'
import DifyColor from '@lobehub/icons/es/Dify/components/Color.js'
import DifyMono from '@lobehub/icons/es/Dify/components/Mono.js'
import * as DifyStyle from '@lobehub/icons/es/Dify/style.js'
import DoubaoColor from '@lobehub/icons/es/Doubao/components/Color.js'
import DoubaoMono from '@lobehub/icons/es/Doubao/components/Mono.js'
import * as DoubaoStyle from '@lobehub/icons/es/Doubao/style.js'
import FastGPTColor from '@lobehub/icons/es/FastGPT/components/Color.js'
import FastGPTMono from '@lobehub/icons/es/FastGPT/components/Mono.js'
import * as FastGPTStyle from '@lobehub/icons/es/FastGPT/style.js'
import GeminiColor from '@lobehub/icons/es/Gemini/components/Color.js'
import GeminiMono from '@lobehub/icons/es/Gemini/components/Mono.js'
import * as GeminiStyle from '@lobehub/icons/es/Gemini/style.js'
import GoogleColor from '@lobehub/icons/es/Google/components/Color.js'
import GoogleMono from '@lobehub/icons/es/Google/components/Mono.js'
import * as GoogleStyle from '@lobehub/icons/es/Google/style.js'
import HunyuanColor from '@lobehub/icons/es/Hunyuan/components/Color.js'
import HunyuanMono from '@lobehub/icons/es/Hunyuan/components/Mono.js'
import * as HunyuanStyle from '@lobehub/icons/es/Hunyuan/style.js'
import JimengColor from '@lobehub/icons/es/Jimeng/components/Color.js'
import JimengMono from '@lobehub/icons/es/Jimeng/components/Mono.js'
import * as JimengStyle from '@lobehub/icons/es/Jimeng/style.js'
import JinaMono from '@lobehub/icons/es/Jina/components/Mono.js'
import * as JinaStyle from '@lobehub/icons/es/Jina/style.js'
import KlingColor from '@lobehub/icons/es/Kling/components/Color.js'
import KlingMono from '@lobehub/icons/es/Kling/components/Mono.js'
import * as KlingStyle from '@lobehub/icons/es/Kling/style.js'
import LobeHubColor from '@lobehub/icons/es/LobeHub/components/Color.js'
import LobeHubMono from '@lobehub/icons/es/LobeHub/components/Mono.js'
import * as LobeHubStyle from '@lobehub/icons/es/LobeHub/style.js'
import MidjourneyMono from '@lobehub/icons/es/Midjourney/components/Mono.js'
import * as MidjourneyStyle from '@lobehub/icons/es/Midjourney/style.js'
import MinimaxColor from '@lobehub/icons/es/Minimax/components/Color.js'
import MinimaxMono from '@lobehub/icons/es/Minimax/components/Mono.js'
import * as MinimaxStyle from '@lobehub/icons/es/Minimax/style.js'
import MistralColor from '@lobehub/icons/es/Mistral/components/Color.js'
import MistralMono from '@lobehub/icons/es/Mistral/components/Mono.js'
import * as MistralStyle from '@lobehub/icons/es/Mistral/style.js'
import MoonshotMono from '@lobehub/icons/es/Moonshot/components/Mono.js'
import * as MoonshotStyle from '@lobehub/icons/es/Moonshot/style.js'
import NewAPIColor from '@lobehub/icons/es/NewAPI/components/Color.js'
import NewAPIMono from '@lobehub/icons/es/NewAPI/components/Mono.js'
import * as NewAPIStyle from '@lobehub/icons/es/NewAPI/style.js'
import OllamaMono from '@lobehub/icons/es/Ollama/components/Mono.js'
import * as OllamaStyle from '@lobehub/icons/es/Ollama/style.js'
import OpenAIColor from '@lobehub/icons/es/OpenAI/components/Color.js'
import OpenAIMono from '@lobehub/icons/es/OpenAI/components/Mono.js'
import * as OpenAIStyle from '@lobehub/icons/es/OpenAI/style.js'
import OpenRouterColor from '@lobehub/icons/es/OpenRouter/components/Color.js'
import OpenRouterMono from '@lobehub/icons/es/OpenRouter/components/Mono.js'
import * as OpenRouterStyle from '@lobehub/icons/es/OpenRouter/style.js'
import OpenWebUIMono from '@lobehub/icons/es/OpenWebUI/components/Mono.js'
import * as OpenWebUIStyle from '@lobehub/icons/es/OpenWebUI/style.js'
import PerplexityColor from '@lobehub/icons/es/Perplexity/components/Color.js'
import PerplexityMono from '@lobehub/icons/es/Perplexity/components/Mono.js'
import * as PerplexityStyle from '@lobehub/icons/es/Perplexity/style.js'
import QwenColor from '@lobehub/icons/es/Qwen/components/Color.js'
import QwenMono from '@lobehub/icons/es/Qwen/components/Mono.js'
import * as QwenStyle from '@lobehub/icons/es/Qwen/style.js'
import ReplicateMono from '@lobehub/icons/es/Replicate/components/Mono.js'
import * as ReplicateStyle from '@lobehub/icons/es/Replicate/style.js'
import SiliconCloudColor from '@lobehub/icons/es/SiliconCloud/components/Color.js'
import SiliconCloudMono from '@lobehub/icons/es/SiliconCloud/components/Mono.js'
import * as SiliconCloudStyle from '@lobehub/icons/es/SiliconCloud/style.js'
import SparkColor from '@lobehub/icons/es/Spark/components/Color.js'
import SparkMono from '@lobehub/icons/es/Spark/components/Mono.js'
import * as SparkStyle from '@lobehub/icons/es/Spark/style.js'
import SunoMono from '@lobehub/icons/es/Suno/components/Mono.js'
import * as SunoStyle from '@lobehub/icons/es/Suno/style.js'
import ViduColor from '@lobehub/icons/es/Vidu/components/Color.js'
import ViduMono from '@lobehub/icons/es/Vidu/components/Mono.js'
import * as ViduStyle from '@lobehub/icons/es/Vidu/style.js'
import VolcengineColor from '@lobehub/icons/es/Volcengine/components/Color.js'
import VolcengineMono from '@lobehub/icons/es/Volcengine/components/Mono.js'
import * as VolcengineStyle from '@lobehub/icons/es/Volcengine/style.js'
import WenxinColor from '@lobehub/icons/es/Wenxin/components/Color.js'
import WenxinMono from '@lobehub/icons/es/Wenxin/components/Mono.js'
import * as WenxinStyle from '@lobehub/icons/es/Wenxin/style.js'
import XAIMono from '@lobehub/icons/es/XAI/components/Mono.js'
import * as XAIStyle from '@lobehub/icons/es/XAI/style.js'
import XinferenceColor from '@lobehub/icons/es/Xinference/components/Color.js'
import XinferenceMono from '@lobehub/icons/es/Xinference/components/Mono.js'
import * as XinferenceStyle from '@lobehub/icons/es/Xinference/style.js'
import YiColor from '@lobehub/icons/es/Yi/components/Color.js'
import YiMono from '@lobehub/icons/es/Yi/components/Mono.js'
import * as YiStyle from '@lobehub/icons/es/Yi/style.js'
import ZhipuColor from '@lobehub/icons/es/Zhipu/components/Color.js'
import ZhipuMono from '@lobehub/icons/es/Zhipu/components/Mono.js'
import * as ZhipuStyle from '@lobehub/icons/es/Zhipu/style.js'
import {
  type ComponentType,
  type ReactNode,
  useEffect,
  useReducer,
} from 'react'

import { IconSub2api } from '@/assets/custom/icon-sub2api'

type IconComponent = ComponentType<Record<string, unknown>>
type CustomIconComponent = ComponentType<{ size?: number }>

const CUSTOM_ICONS: Record<string, CustomIconComponent> = {
  Sub2API: IconSub2api,
}

type LobeIconStyle = {
  AVATAR_BACKGROUND: string
  AVATAR_COLOR: string
  AVATAR_ICON_MULTIPLE: number
  TITLE: string
}

type CommonLobeIcon = {
  Color?: IconComponent
  Mono: IconComponent
  style: LobeIconStyle
}

const COMMON_LOBE_ICONS: Record<string, CommonLobeIcon> = {
  Ai360: { Color: Ai360Color, Mono: Ai360Mono, style: Ai360Style },
  Anthropic: { Mono: AnthropicMono, style: AnthropicStyle },
  Aws: { Color: AwsColor, Mono: AwsMono, style: AwsStyle },
  Azure: { Color: AzureColor, Mono: AzureMono, style: AzureStyle },
  AzureAI: { Color: AzureAIColor, Mono: AzureAIMono, style: AzureAIStyle },
  Baidu: { Color: BaiduColor, Mono: BaiduMono, style: BaiduStyle },
  Claude: { Color: ClaudeColor, Mono: ClaudeMono, style: ClaudeStyle },
  Cline: { Mono: ClineMono, style: ClineStyle },
  Cloudflare: {
    Color: CloudflareColor,
    Mono: CloudflareMono,
    style: CloudflareStyle,
  },
  Cohere: { Color: CohereColor, Mono: CohereMono, style: CohereStyle },
  Coze: { Mono: CozeMono, style: CozeStyle },
  DeepSeek: {
    Color: DeepSeekColor,
    Mono: DeepSeekMono,
    style: DeepSeekStyle,
  },
  Dify: { Color: DifyColor, Mono: DifyMono, style: DifyStyle },
  Doubao: { Color: DoubaoColor, Mono: DoubaoMono, style: DoubaoStyle },
  FastGPT: { Color: FastGPTColor, Mono: FastGPTMono, style: FastGPTStyle },
  Gemini: { Color: GeminiColor, Mono: GeminiMono, style: GeminiStyle },
  Google: { Color: GoogleColor, Mono: GoogleMono, style: GoogleStyle },
  Hunyuan: { Color: HunyuanColor, Mono: HunyuanMono, style: HunyuanStyle },
  Jimeng: { Color: JimengColor, Mono: JimengMono, style: JimengStyle },
  Jina: { Mono: JinaMono, style: JinaStyle },
  Kling: { Color: KlingColor, Mono: KlingMono, style: KlingStyle },
  LobeHub: { Color: LobeHubColor, Mono: LobeHubMono, style: LobeHubStyle },
  Midjourney: { Mono: MidjourneyMono, style: MidjourneyStyle },
  Minimax: { Color: MinimaxColor, Mono: MinimaxMono, style: MinimaxStyle },
  Mistral: { Color: MistralColor, Mono: MistralMono, style: MistralStyle },
  Moonshot: { Mono: MoonshotMono, style: MoonshotStyle },
  NewAPI: { Color: NewAPIColor, Mono: NewAPIMono, style: NewAPIStyle },
  Ollama: { Mono: OllamaMono, style: OllamaStyle },
  OpenAI: { Color: OpenAIColor, Mono: OpenAIMono, style: OpenAIStyle },
  OpenRouter: {
    Color: OpenRouterColor,
    Mono: OpenRouterMono,
    style: OpenRouterStyle,
  },
  OpenWebUI: { Mono: OpenWebUIMono, style: OpenWebUIStyle },
  Perplexity: {
    Color: PerplexityColor,
    Mono: PerplexityMono,
    style: PerplexityStyle,
  },
  Qwen: { Color: QwenColor, Mono: QwenMono, style: QwenStyle },
  Replicate: { Mono: ReplicateMono, style: ReplicateStyle },
  SiliconCloud: {
    Color: SiliconCloudColor,
    Mono: SiliconCloudMono,
    style: SiliconCloudStyle,
  },
  Spark: { Color: SparkColor, Mono: SparkMono, style: SparkStyle },
  Suno: { Mono: SunoMono, style: SunoStyle },
  Vidu: { Color: ViduColor, Mono: ViduMono, style: ViduStyle },
  Volcengine: {
    Color: VolcengineColor,
    Mono: VolcengineMono,
    style: VolcengineStyle,
  },
  Wenxin: { Color: WenxinColor, Mono: WenxinMono, style: WenxinStyle },
  XAI: { Mono: XAIMono, style: XAIStyle },
  Xinference: {
    Color: XinferenceColor,
    Mono: XinferenceMono,
    style: XinferenceStyle,
  },
  Yi: { Color: YiColor, Mono: YiMono, style: YiStyle },
  Zhipu: { Color: ZhipuColor, Mono: ZhipuMono, style: ZhipuStyle },
}

type LoadedFallbackIcon = {
  component: IconComponent
}

const fallbackLobeIcons = new Map<string, LoadedFallbackIcon | null>()
const fallbackLobeIconPromises = new Map<
  string,
  Promise<LoadedFallbackIcon | null>
>()
const commonIconVariants = new Set(['Avatar', 'Color', 'Mono'])
let fallbackLobeIconModulePromise: Promise<Record<string, unknown>> | undefined

function loadFallbackLobeIconModule(): Promise<Record<string, unknown>> {
  fallbackLobeIconModulePromise ??= import('@lobehub/icons/es/icons.js').then(
    (module) => module as Record<string, unknown>
  )
  return fallbackLobeIconModulePromise
}

function loadFallbackLobeIcon(
  baseKey: string,
  requestedVariant: string | undefined
): Promise<LoadedFallbackIcon | null> {
  if (
    !/^[A-Za-z][A-Za-z0-9]*$/.test(baseKey) ||
    (requestedVariant !== undefined &&
      !/^[A-Z][A-Za-z0-9]*$/.test(requestedVariant))
  ) {
    return Promise.resolve(null)
  }

  const variant = requestedVariant ?? 'Mono'
  const fallbackKey = `${baseKey}.${variant}`
  const existing = fallbackLobeIconPromises.get(fallbackKey)
  if (existing) return existing

  let promise = loadFallbackLobeIconModule()
    .then((module) => {
      const baseIcon = module[baseKey] as
        | (IconComponent & Record<string, unknown>)
        | undefined
      if (!baseIcon) return null

      const requestedComponent = baseIcon[variant]
      const component = (
        typeof requestedComponent === 'function' ||
        typeof requestedComponent === 'object'
          ? requestedComponent
          : baseIcon
      ) as IconComponent
      return { component }
    })
    .catch(() => null)

  promise = promise.then((icon) => {
    fallbackLobeIcons.set(fallbackKey, icon)
    return icon
  })
  fallbackLobeIconPromises.set(fallbackKey, promise)
  return promise
}

type LobeAvatarProps = {
  baseKey: string
  componentProps: Record<string, string | number | boolean>
  definition: CommonLobeIcon
}

function LobeAvatar(avatarProps: LobeAvatarProps) {
  const size =
    typeof avatarProps.componentProps.size === 'number'
      ? avatarProps.componentProps.size
      : 20
  const shape =
    avatarProps.componentProps.shape === 'square' ? 'square' : 'circle'
  let background = avatarProps.definition.style.AVATAR_BACKGROUND

  if (avatarProps.baseKey === 'OpenAI') {
    switch (avatarProps.componentProps.type) {
      case 'gpt3':
        background = OpenAIStyle.COLOR_GPT_3
        break
      case 'gpt4':
        background = OpenAIStyle.COLOR_GPT_4
        break
      case 'gpt5':
        background = OpenAIStyle.COLOR_GPT_5
        break
      case 'o1':
      case 'o3':
        background = OpenAIStyle.COLOR_O_1
        break
      case 'oss':
        background = OpenAIStyle.COLOR_OSS
        break
      case 'platform':
        background = OpenAIStyle.COLOR_PLATFORM
        break
    }
  }

  const MonoIcon = avatarProps.definition.Mono

  return (
    <span
      aria-label={avatarProps.definition.style.TITLE}
      className={
        typeof avatarProps.componentProps.className === 'string'
          ? avatarProps.componentProps.className
          : undefined
      }
      style={{
        alignItems: 'center',
        background,
        borderRadius: shape === 'circle' ? '50%' : Math.floor(size * 0.1),
        color: avatarProps.definition.style.AVATAR_COLOR,
        display: 'inline-flex',
        flex: 'none',
        height: size,
        justifyContent: 'center',
        overflow: 'hidden',
        width: size,
      }}
    >
      <MonoIcon
        aria-hidden='true'
        color={avatarProps.definition.style.AVATAR_COLOR}
        size={size}
        style={{
          transform: `scale(${avatarProps.definition.style.AVATAR_ICON_MULTIPLE})`,
        }}
      />
    </span>
  )
}

/**
 * Parse a property value from string to appropriate type
 * @param raw - Raw string value
 * @returns Parsed value (boolean, number, or string)
 */
function parseValue(raw: string | undefined | null): string | number | boolean {
  if (raw == null) return true

  let v = String(raw).trim()

  // Remove curly braces
  if (v.startsWith('{') && v.endsWith('}')) {
    v = v.slice(1, -1).trim()
  }

  // Remove quotes
  if (
    (v.startsWith('"') && v.endsWith('"')) ||
    (v.startsWith("'") && v.endsWith("'"))
  ) {
    return v.slice(1, -1)
  }

  // Boolean
  if (v === 'true') return true
  if (v === 'false') return false

  // Number
  if (/^-?\d+(?:\.\d+)?$/.test(v)) return Number(v)

  // Return as string
  return v
}

/**
 * Get LobeHub icon component by name
 * @param iconName - Icon name/description (e.g., "OpenAI", "OpenAI.Color", "Claude.Avatar")
 * @param size - Icon size (default: 20)
 * @returns Icon component or fallback
 *
 * @example
 * getLobeIcon("OpenAI", 24)
 * getLobeIcon("OpenAI.Color", 20)
 * getLobeIcon("Claude.Avatar.type={'platform'}", 32)
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): ReactNode {
  return <LobeIcon iconName={iconName} size={size} />
}

type LobeIconProps = {
  iconName: string | undefined | null
  size: number
}

function LobeIcon(iconProps: LobeIconProps) {
  const [, rerender] = useReducer((version: number) => version + 1, 0)
  const trimmedName =
    typeof iconProps.iconName === 'string' ? iconProps.iconName.trim() : ''
  const segments = trimmedName.split('.')
  const baseKey = segments[0]
  const requestedVariant =
    segments.length > 1 && /^[A-Z]/.test(segments[1]) ? segments[1] : undefined
  const CustomIcon = baseKey ? CUSTOM_ICONS[baseKey] : undefined
  const commonIcon = baseKey ? COMMON_LOBE_ICONS[baseKey] : undefined
  const fallbackKey = baseKey ? `${baseKey}.${requestedVariant ?? 'Mono'}` : ''
  const fallbackIcon = fallbackKey
    ? fallbackLobeIcons.get(fallbackKey)
    : undefined
  const fallbackResolved = fallbackKey
    ? fallbackLobeIcons.has(fallbackKey)
    : false
  const needsFallback =
    !CustomIcon &&
    (!commonIcon ||
      (requestedVariant !== undefined &&
        !commonIconVariants.has(requestedVariant)))

  useEffect(() => {
    if (!baseKey || !needsFallback || fallbackResolved) return

    let active = true
    void loadFallbackLobeIcon(baseKey, requestedVariant).then(() => {
      if (active) rerender()
    })
    return () => {
      active = false
    }
  }, [baseKey, fallbackResolved, needsFallback, requestedVariant])

  if (!trimmedName) {
    return (
      <div
        className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
        style={{ width: iconProps.size, height: iconProps.size }}
      >
        ?
      </div>
    )
  }

  if (CustomIcon) {
    return <CustomIcon size={iconProps.size} />
  }

  let IconComponent: IconComponent | undefined
  let renderAvatar = false
  let avatarDefinition: CommonLobeIcon | undefined
  let propStartIndex: number

  if (commonIcon && !needsFallback) {
    renderAvatar = requestedVariant === 'Avatar'
    avatarDefinition = renderAvatar ? commonIcon : undefined
    IconComponent =
      requestedVariant === 'Color'
        ? (commonIcon.Color ?? commonIcon.Mono)
        : commonIcon.Mono
    propStartIndex = requestedVariant ? 2 : 1
  } else {
    IconComponent = fallbackIcon?.component
    propStartIndex = requestedVariant ? 2 : 1
  }

  // Fallback if icon not found
  if (
    (!IconComponent && !renderAvatar) ||
    (typeof IconComponent !== 'function' && typeof IconComponent !== 'object')
  ) {
    const firstLetter = trimmedName.charAt(0).toUpperCase()
    return (
      <div
        className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
        style={{ width: iconProps.size, height: iconProps.size }}
      >
        {firstLetter}
      </div>
    )
  }

  // Parse chained properties (e.g., "type={'platform'}", "shape='square'")
  const componentProps: Record<string, string | number | boolean> = {}

  for (let i = propStartIndex; i < segments.length; i++) {
    const seg = segments[i]
    if (!seg) continue

    const eqIdx = seg.indexOf('=')
    if (eqIdx === -1) {
      componentProps[seg.trim()] = true
      continue
    }

    const key = seg.slice(0, eqIdx).trim()
    const valRaw = seg.slice(eqIdx + 1).trim()
    componentProps[key] = parseValue(valRaw)
  }

  // Set size if not explicitly specified in the string
  if (componentProps.size == null) {
    componentProps.size = iconProps.size
  }

  if (renderAvatar && avatarDefinition) {
    return (
      <LobeAvatar
        baseKey={baseKey}
        componentProps={componentProps}
        definition={avatarDefinition}
      />
    )
  }

  return <IconComponent {...componentProps} />
}
