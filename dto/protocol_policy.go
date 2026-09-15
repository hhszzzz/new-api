package dto

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

const ProtocolPolicyVersion = 1
const (
	ProtocolConversionNative   = "native_only"
	ProtocolConversionLossless = "lossless"
	ProtocolConversionSafe     = "safe"
	ProtocolSelectionDeclared  = "declared"
	ProtocolSelectionAutomatic = "auto"
	ProtocolRequestStructured  = "structured"
	ProtocolRequestPassthrough = "passthrough"
	ProtocolStateDisabled      = "disabled"
	ProtocolStateBridge        = "bridge"
	ProtocolStateAll           = "all"
)

// ProtocolPolicy has sparse channel overrides and ordered model rules. Empty
// fields inherit; false/zero are never used to mean both inherit and disable.
type ProtocolPolicy struct {
	Version           int                 `json:"version"`
	Conversion        string              `json:"conversion,omitempty"`
	Selection         string              `json:"selection,omitempty"`
	UpstreamProtocols []string            `json:"upstream_protocols,omitempty"`
	RequestMode       string              `json:"request_mode,omitempty"`
	StateScope        string              `json:"state_scope,omitempty"`
	StateTTLSeconds   int                 `json:"state_ttl_seconds,omitempty"`
	MaxStateTurns     int                 `json:"max_state_turns,omitempty"`
	MaxStateBytes     int                 `json:"max_state_bytes,omitempty"`
	Rules             []ProtocolModelRule `json:"rules,omitempty"`
}

type ProtocolModelRule struct {
	Deny              bool                  `json:"deny,omitempty"`
	RequireStructured bool                  `json:"require_structured,omitempty"`
	ModelPattern      string                `json:"model_pattern,omitempty"`
	RequestProtocol   relayconvert.Protocol `json:"request_protocol,omitempty"`
	TargetProtocol    relayconvert.Protocol `json:"target_protocol,omitempty"`
	ChannelIDs        []int                 `json:"channel_ids,omitempty"`
	ChannelTypes      []int                 `json:"channel_types,omitempty"`
	Conversion        string                `json:"conversion,omitempty"`
	UpstreamProtocols []string              `json:"upstream_protocols,omitempty"`
}

func DefaultProtocolPolicy() ProtocolPolicy {
	return ProtocolPolicy{Version: ProtocolPolicyVersion, Conversion: ProtocolConversionSafe,
		Selection: ProtocolSelectionDeclared, RequestMode: ProtocolRequestStructured,
		StateScope: ProtocolStateBridge, StateTTLSeconds: 86400, MaxStateTurns: 128, MaxStateBytes: 4 * 1024 * 1024}
}

func (p *ProtocolPolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.Version != ProtocolPolicyVersion {
		return fmt.Errorf("unsupported protocol_policy version %d", p.Version)
	}
	for _, field := range []struct {
		name, value string
		allowed     []string
	}{
		{"conversion", p.Conversion, []string{ProtocolConversionNative, ProtocolConversionLossless, ProtocolConversionSafe}},
		{"selection", p.Selection, []string{ProtocolSelectionDeclared, ProtocolSelectionAutomatic}},
		{"request_mode", p.RequestMode, []string{ProtocolRequestStructured, ProtocolRequestPassthrough}},
		{"state_scope", p.StateScope, []string{ProtocolStateDisabled, ProtocolStateBridge, ProtocolStateAll}},
	} {
		if field.value != "" && !slices.Contains(field.allowed, field.value) {
			return fmt.Errorf("protocol_policy.%s has unsupported value %q", field.name, field.value)
		}
	}
	if err := validateProtocolCapabilityList("protocol_policy.upstream_protocols", p.UpstreamProtocols); err != nil {
		return err
	}
	if p.StateTTLSeconds != 0 && (p.StateTTLSeconds < 60 || p.StateTTLSeconds > 30*86400) {
		return fmt.Errorf("protocol_policy.state_ttl_seconds must be between 60 and 2592000")
	}
	if p.MaxStateTurns != 0 && (p.MaxStateTurns < 1 || p.MaxStateTurns > 4096) {
		return fmt.Errorf("protocol_policy.max_state_turns must be between 1 and 4096")
	}
	if p.MaxStateBytes != 0 && (p.MaxStateBytes < 1024 || p.MaxStateBytes > 128*1024*1024) {
		return fmt.Errorf("protocol_policy.max_state_bytes must be between 1024 and 134217728")
	}
	for i, rule := range p.Rules {
		if rule.ModelPattern != "" {
			if _, err := regexp.Compile(rule.ModelPattern); err != nil {
				return fmt.Errorf("protocol_policy.rules[%d].model_pattern: %w", i, err)
			}
		}
		if rule.Conversion != "" && !slices.Contains([]string{ProtocolConversionNative, ProtocolConversionLossless, ProtocolConversionSafe}, rule.Conversion) {
			return fmt.Errorf("protocol_policy.rules[%d].conversion is invalid", i)
		}
		if err := validateProtocolCapabilityList(fmt.Sprintf("protocol_policy.rules[%d].upstream_protocols", i), rule.UpstreamProtocols); err != nil {
			return err
		}
		for _, protocol := range []relayconvert.Protocol{rule.RequestProtocol, rule.TargetProtocol} {
			if protocol != "" {
				if _, ok := protocol.RelayFormat(); !ok {
					return fmt.Errorf("protocol_policy.rules[%d] has unknown protocol %q", i, protocol)
				}
			}
		}
	}
	return nil
}

// ResolveProtocolPolicy applies global defaults, then the channel override. The
// first matching rule at each level wins, with channel rules taking precedence.
func ResolveProtocolPolicy(global ProtocolPolicy, channel *ProtocolPolicy, model string, protocol relayconvert.Protocol, channelID, channelType int) (ProtocolPolicy, relayconvert.Protocol, bool) {
	resolved := DefaultProtocolPolicy()
	var target relayconvert.Protocol
	var denied bool
	for _, layer := range []*ProtocolPolicy{&global, channel} {
		if layer == nil {
			continue
		}
		if layer.Conversion != "" {
			resolved.Conversion = layer.Conversion
		}
		if layer.Selection != "" {
			resolved.Selection = layer.Selection
		}
		if layer.RequestMode != "" {
			resolved.RequestMode = layer.RequestMode
		}
		if layer.StateScope != "" {
			resolved.StateScope = layer.StateScope
		}
		if layer.StateTTLSeconds != 0 {
			resolved.StateTTLSeconds = layer.StateTTLSeconds
		}
		if layer.MaxStateTurns != 0 {
			resolved.MaxStateTurns = layer.MaxStateTurns
		}
		if layer.MaxStateBytes != 0 {
			resolved.MaxStateBytes = layer.MaxStateBytes
		}
		if len(layer.UpstreamProtocols) > 0 {
			resolved.UpstreamProtocols = slices.Clone(layer.UpstreamProtocols)
		}
	}
	for _, layer := range []*ProtocolPolicy{&global, channel} {
		if layer == nil {
			continue
		}
		for _, rule := range layer.Rules {
			if rule.RequireStructured && resolved.RequestMode != ProtocolRequestStructured {
				continue
			}
			if rule.RequestProtocol != "" && rule.RequestProtocol != protocol {
				continue
			}
			if len(rule.ChannelIDs) > 0 && !slices.Contains(rule.ChannelIDs, channelID) && !slices.Contains(rule.ChannelTypes, channelType) {
				continue
			}
			if len(rule.ChannelIDs) == 0 && len(rule.ChannelTypes) > 0 && !slices.Contains(rule.ChannelTypes, channelType) {
				continue
			}
			if rule.ModelPattern != "" {
				matched, err := regexp.MatchString(rule.ModelPattern, strings.TrimSpace(model))
				if err != nil || !matched {
					continue
				}
			}
			if rule.Conversion != "" {
				resolved.Conversion = rule.Conversion
			}
			if len(rule.UpstreamProtocols) > 0 {
				resolved.UpstreamProtocols = slices.Clone(rule.UpstreamProtocols)
			}
			if rule.TargetProtocol != "" {
				target = rule.TargetProtocol
			}
			denied = rule.Deny
			break
		}
	}
	return resolved, target, denied
}
