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
	if !found || !isAffinityProtocol(protocol) {
		return "", false
	}
	return protocol, true
}

func RememberProtocolAffinity(channel *model.Channel, effectiveModel string, requestProtocol, upstreamProtocol Protocol) {
	if !isAffinityProtocol(upstreamProtocol) {
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
	if apiError.HasProtocolUnsupportedEvidence() {
		return true
	}
	switch apiError.StatusCode {
	case http.StatusNotFound:
		if apiError.ProtocolUnsupportedChecked() {
			return false
		}
		message := strings.ToLower(strings.TrimSpace(apiError.Error()))
		// A bare 404 is the usual response from nginx and compatible gateways
		// when the probed wire endpoint does not exist. Resource-specific 404s
		// are not protocol evidence and must stay on the normal retry path.
		return message == "not found" || message == "404 not found" ||
			strings.HasPrefix(message, "bad response status code 404")
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
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

func isAffinityProtocol(protocol Protocol) bool {
	switch protocol {
	case ProtocolChat, ProtocolMessages, ProtocolResponses, ProtocolGemini:
		return true
	default:
		return false
	}
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
