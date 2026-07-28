package model_setting

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultProtocolBridgeStateTTLSeconds = 3600
	DefaultProtocolBridgeMaxStateTurns   = 128
	DefaultProtocolBridgeMaxStateBytes   = 4 * 1024 * 1024

	MinProtocolBridgeStateTTLSeconds = 60
	MaxProtocolBridgeStateTTLSeconds = 24 * 60 * 60
	MinProtocolBridgeMaxStateTurns   = 1
	MaxProtocolBridgeMaxStateTurns   = 512
	MinProtocolBridgeMaxStateBytes   = 64 * 1024
	MaxProtocolBridgeMaxStateBytes   = 16 * 1024 * 1024
)

type ProtocolBridgePolicy struct {
	Enabled                bool `json:"enabled"`
	DefaultAllowConversion bool `json:"default_allow_conversion"`
	StateTTLSeconds        int  `json:"state_ttl_seconds"`
	MaxStateTurns          int  `json:"max_state_turns"`
	MaxStateBytes          int  `json:"max_state_bytes"`
}

func (p ProtocolBridgePolicy) Validate() error {
	if p.StateTTLSeconds < MinProtocolBridgeStateTTLSeconds || p.StateTTLSeconds > MaxProtocolBridgeStateTTLSeconds {
		return fmt.Errorf("protocol_bridge_policy.state_ttl_seconds must be between %d and %d", MinProtocolBridgeStateTTLSeconds, MaxProtocolBridgeStateTTLSeconds)
	}
	if p.MaxStateTurns < MinProtocolBridgeMaxStateTurns || p.MaxStateTurns > MaxProtocolBridgeMaxStateTurns {
		return fmt.Errorf("protocol_bridge_policy.max_state_turns must be between %d and %d", MinProtocolBridgeMaxStateTurns, MaxProtocolBridgeMaxStateTurns)
	}
	if p.MaxStateBytes < MinProtocolBridgeMaxStateBytes || p.MaxStateBytes > MaxProtocolBridgeMaxStateBytes {
		return fmt.Errorf("protocol_bridge_policy.max_state_bytes must be between %d and %d", MinProtocolBridgeMaxStateBytes, MaxProtocolBridgeMaxStateBytes)
	}
	return nil
}

type ChatCompletionsToResponsesPolicy struct {
	Enabled       bool     `json:"enabled"`
	AllChannels   bool     `json:"all_channels"`
	ChannelIDs    []int    `json:"channel_ids,omitempty"`
	ChannelTypes  []int    `json:"channel_types,omitempty"`
	ModelPatterns []string `json:"model_patterns,omitempty"`
}

func (p ChatCompletionsToResponsesPolicy) IsChannelEnabled(channelID int, channelType int) bool {
	if !p.Enabled {
		return false
	}
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                             `json:"pass_through_request_enabled"`
	ThinkingModelBlacklist           []string                         `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ChatCompletionsToResponsesPolicy `json:"chat_completions_to_responses_policy"`
	ProtocolBridgePolicy             ProtocolBridgePolicy             `json:"protocol_bridge_policy"`
}

// 默认配置
var defaultOpenaiSettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Enabled:     false,
		AllChannels: true,
	},
	ProtocolBridgePolicy: ProtocolBridgePolicy{
		Enabled:                false,
		DefaultAllowConversion: false,
		StateTTLSeconds:        DefaultProtocolBridgeStateTTLSeconds,
		MaxStateTurns:          DefaultProtocolBridgeMaxStateTurns,
		MaxStateBytes:          DefaultProtocolBridgeMaxStateBytes,
	},
}

// 全局实例
var globalSettings = defaultOpenaiSettings

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("global", &globalSettings)
}

func GetGlobalSettings() *GlobalSettings {
	return &globalSettings
}

func (s *GlobalSettings) ValidateConfig() error {
	return s.ProtocolBridgePolicy.Validate()
}

// ShouldPreserveThinkingSuffix 判断模型是否配置为保留 thinking/-nothinking/-low/-high/-medium 后缀
func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}

	for _, entry := range globalSettings.ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}
