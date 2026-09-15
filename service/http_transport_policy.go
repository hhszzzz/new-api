package service

import (
	"context"
	"fmt"
	hostdto "github.com/QuantumNous/new-api/dto"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/logger"
)

// HTTPTransportPolicy is the runtime-normalized outbound HTTP transport policy
// for a channel. Unknown or out-of-range stored values are clamped safely.
type HTTPTransportPolicy struct {
	Protocol string // hostdto.HTTPProtocolAuto or hostdto.HTTPProtocolHTTP1
	Shards   int    // 1..hostdto.MaxHTTP2ConnectionShards
}

var httpTransportPolicyWarnings sync.Map

func defaultHTTPTransportPolicy() HTTPTransportPolicy {
	return HTTPTransportPolicy{
		Protocol: hostdto.HTTPProtocolAuto,
		Shards:   1,
	}
}

// NormalizeHTTPTransportPolicy converts channel settings into a safe runtime policy.
// Invalid stored values never panic; they clamp to defaults and warn once per bad value.
func NormalizeHTTPTransportPolicy(settings hostdto.ChannelSettings) HTTPTransportPolicy {
	policy := defaultHTTPTransportPolicy()

	protocol := strings.ToLower(strings.TrimSpace(settings.HTTPProtocol))
	switch protocol {
	case "", hostdto.HTTPProtocolAuto:
		policy.Protocol = hostdto.HTTPProtocolAuto
	case hostdto.HTTPProtocolHTTP1:
		policy.Protocol = hostdto.HTTPProtocolHTTP1
	default:
		warnHTTPTransportPolicyOnce("http_protocol", settings.HTTPProtocol)
		policy.Protocol = hostdto.HTTPProtocolAuto
	}

	shards := settings.HTTP2ConnectionShards
	switch {
	case shards == 0:
		policy.Shards = 1
	case shards < 1:
		warnHTTPTransportPolicyOnce("http2_connection_shards", fmt.Sprintf("%d", shards))
		policy.Shards = 1
	case shards > hostdto.MaxHTTP2ConnectionShards:
		warnHTTPTransportPolicyOnce("http2_connection_shards", fmt.Sprintf("%d", shards))
		policy.Shards = hostdto.MaxHTTP2ConnectionShards
	default:
		policy.Shards = shards
	}

	if policy.Protocol == hostdto.HTTPProtocolHTTP1 {
		if settings.HTTP2ConnectionShards > 1 {
			warnHTTPTransportPolicyOnce(
				"http_protocol+http2_connection_shards",
				fmt.Sprintf("%s+%d", hostdto.HTTPProtocolHTTP1, settings.HTTP2ConnectionShards),
			)
		}
		policy.Shards = 1
	}
	if policy.Shards < 1 {
		policy.Shards = 1
	}
	return policy
}

func warnHTTPTransportPolicyOnce(field, value string) {
	key := field + "=" + value
	if _, loaded := httpTransportPolicyWarnings.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	logger.LogWarn(
		context.Background(),
		fmt.Sprintf("invalid channel http transport setting clamped: %s=%q", field, value),
	)
}

func (p HTTPTransportPolicy) cacheKeyPart() string {
	return fmt.Sprintf("%s|%d", p.Protocol, p.Shards)
}

func (p HTTPTransportPolicy) String() string {
	return p.cacheKeyPart()
}
