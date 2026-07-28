package channelcompat

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/samber/hot"
)

const (
	protocolAffinityNamespace cachex.Namespace = "protocol_bridge:wire_affinity:v1"
	protocolAffinityCapacity                   = 100_000
)

var (
	protocolAffinityCacheOnce sync.Once
	protocolAffinityCache     *cachex.HybridCache[string]
)

func LookupProtocolAffinity(channel *model.Channel, effectiveModel string, requestProtocol Protocol) (Protocol, bool) {
	key, ok := protocolAffinityKey(channel, effectiveModel, requestProtocol)
	if !ok {
		return "", false
	}
	value, found, err := getProtocolAffinityCache().Get(key)
	if err != nil {
		common.SysError(fmt.Sprintf("protocol affinity cache lookup failed: %v", err))
		return "", false
	}
	protocol := Protocol(strings.TrimSpace(value))
	if !found || (protocol != ProtocolChat && protocol != ProtocolMessages && protocol != ProtocolResponses) {
		return "", false
	}
	return protocol, true
}

func RememberProtocolAffinity(channel *model.Channel, effectiveModel string, requestProtocol, upstreamProtocol Protocol) {
	if upstreamProtocol != ProtocolChat && upstreamProtocol != ProtocolMessages && upstreamProtocol != ProtocolResponses {
		return
	}
	key, ok := protocolAffinityKey(channel, effectiveModel, requestProtocol)
	if !ok {
		return
	}
	ttl := model_setting.GetGlobalSettings().ProtocolBridgePolicy.StateTTLSeconds
	if ttl <= 0 {
		ttl = model_setting.DefaultProtocolBridgeStateTTLSeconds
	}
	if err := getProtocolAffinityCache().SetWithTTL(key, string(upstreamProtocol), time.Duration(ttl)*time.Second); err != nil {
		common.SysError(fmt.Sprintf("protocol affinity cache write failed: %v", err))
	}
}

func ForgetProtocolAffinity(channel *model.Channel, effectiveModel string, requestProtocol Protocol) {
	key, ok := protocolAffinityKey(channel, effectiveModel, requestProtocol)
	if !ok {
		return
	}
	if _, err := getProtocolAffinityCache().DeleteMany([]string{key}); err != nil {
		common.SysError(fmt.Sprintf("protocol affinity cache delete failed: %v", err))
	}
}

func IsProtocolUnsupportedError(apiError *types.NewAPIError) bool {
	if apiError == nil {
		return false
	}
	switch apiError.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}

	message := strings.ToLower(strings.TrimSpace(apiError.Error()))
	if message == "" {
		return false
	}
	for _, marker := range []string{
		"unsupported endpoint",
		"endpoint not supported",
		"unknown endpoint",
		"unrecognized endpoint",
		"method not allowed",
		"not implemented",
		"unsupported protocol",
		"protocol not supported",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	mentionsWireAPI := false
	for _, marker := range []string{
		"/v1/responses",
		"/responses",
		"responses api",
		"/v1/messages",
		"/messages",
		"messages api",
		"/v1/chat/completions",
		"/chat/completions",
		"chat completions",
	} {
		if strings.Contains(message, marker) {
			mentionsWireAPI = true
			break
		}
	}
	if !mentionsWireAPI {
		return false
	}
	for _, marker := range []string{
		"not supported",
		"does not support",
		"unsupported",
		"not found",
		"does not exist",
		"unknown route",
		"only supports",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func protocolAffinityKey(channel *model.Channel, effectiveModel string, requestProtocol Protocol) (string, bool) {
	if channel == nil || channel.Id <= 0 {
		return "", false
	}
	if requestProtocol != ProtocolMessages && requestProtocol != ProtocolResponses {
		return "", false
	}
	capabilities := channel.GetOtherSettings().ProtocolCapabilities
	if capabilities == nil || capabilities.GetSelectionMode() != dto.ProtocolSelectionModeAuto {
		return "", false
	}
	protocols, allowConversion := capabilities.Resolve(strings.TrimSpace(effectiveModel))
	conversionMode := "inherit"
	if allowConversion != nil {
		conversionMode = fmt.Sprintf("%t", *allowConversion)
	}
	fingerprintInput := strings.Join([]string{
		fmt.Sprintf("channel=%d", channel.Id),
		fmt.Sprintf("type=%d", channel.Type),
		"base_url=" + strings.TrimSpace(channel.GetBaseURL()),
		"model=" + strings.TrimSpace(effectiveModel),
		"request_protocol=" + string(requestProtocol),
		"selection_mode=" + capabilities.GetSelectionMode(),
		"protocols=" + strings.Join(protocols, ","),
		"allow_conversion=" + conversionMode,
	}, "\n")
	sum := sha256.Sum256([]byte(fingerprintInput))
	return fmt.Sprintf("%d:%x", channel.Id, sum[:]), true
}

func getProtocolAffinityCache() *cachex.HybridCache[string] {
	protocolAffinityCacheOnce.Do(func() {
		protocolAffinityCache = cachex.NewHybridCache(cachex.HybridCacheConfig[string]{
			Namespace:  protocolAffinityNamespace,
			Redis:      common.RDB,
			RedisCodec: cachex.StringCodec{},
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, protocolAffinityCapacity).
					WithTTL(time.Duration(model_setting.DefaultProtocolBridgeStateTTLSeconds) * time.Second).
					WithJanitor().
					Build()
			},
		})
	})
	return protocolAffinityCache
}
