package common

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// The route-injected prompt must outrank both the channel system prompt and any
// system prompt the caller sent, because a routed request runs on a different
// upstream model than the client asked for. The channel prompt keeps its
// existing opt-in behavior so enabling routes cannot silently change how
// already-configured channels treat a caller-supplied system prompt.
func TestLeadingSystemPromptResolvesLayersByPriority(t *testing.T) {
	const routePrompt = "You are model-one."
	const channelPrompt = "Channel policy."

	routedInfo := func(override bool) *RelayInfo {
		info := &RelayInfo{
			UserModelRouteId:     7,
			RouteTargetModelName: "model-two",
			RouteInjectPrompt:    routePrompt,
		}
		info.ChannelMeta = &ChannelMeta{ChannelSetting: dto.ChannelSettings{
			SystemPrompt:         channelPrompt,
			SystemPromptOverride: override,
		}}
		return info
	}

	tests := []struct {
		name            string
		info            *RelayInfo
		hasClientPrompt bool
		expected        string
	}{
		{
			name:            "route prompt leads the channel prompt without a client prompt",
			info:            routedInfo(false),
			hasClientPrompt: false,
			expected:        routePrompt + "\n" + channelPrompt,
		},
		{
			name:            "route prompt still applies when the client sent a system prompt",
			info:            routedInfo(false),
			hasClientPrompt: true,
			expected:        routePrompt,
		},
		{
			name:            "channel prompt joins the route prompt when override is enabled",
			info:            routedInfo(true),
			hasClientPrompt: true,
			expected:        routePrompt + "\n" + channelPrompt,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.info.LeadingSystemPrompt(test.hasClientPrompt))
		})
	}
}

// An inactive or absent route must never inject, so a stale context value
// cannot leak a previous request's identity prompt into an unrouted request.
func TestRouteSystemPromptRequiresAnActiveRoute(t *testing.T) {
	tests := []struct {
		name string
		info *RelayInfo
	}{
		{name: "nil info", info: nil},
		{
			name: "prompt without a route",
			info: &RelayInfo{RouteInjectPrompt: "You are model-one."},
		},
		{
			name: "route id without a target model",
			info: &RelayInfo{UserModelRouteId: 7, RouteInjectPrompt: "You are model-one."},
		},
		{
			name: "blank prompt on an active route",
			info: &RelayInfo{
				UserModelRouteId:     7,
				RouteTargetModelName: "model-two",
				RouteInjectPrompt:    "   ",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Empty(t, test.info.RouteSystemPrompt())
			assert.Empty(t, test.info.LeadingSystemPrompt(false))
			assert.Empty(t, test.info.LeadingSystemPrompt(true))
		})
	}
}

// A single request can pass through several stages that each compose system
// prompts: the chat handler folds them into a system message, the chat ->
// responses converter maps that message into `instructions`, and the responses
// handler reads `instructions` back. Without the applied-once guard the same
// prompt would reach the upstream twice.
func TestLeadingSystemPromptAppliesOnlyOncePerRequest(t *testing.T) {
	const routePrompt = "You are model-one."
	info := &RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "model-two",
		RouteInjectPrompt:    routePrompt,
	}
	info.ChannelMeta = &ChannelMeta{ChannelSetting: dto.ChannelSettings{
		SystemPrompt:         "Channel policy.",
		SystemPromptOverride: true,
	}}

	assert.Equal(t, routePrompt+"\nChannel policy.", info.LeadingSystemPrompt(false))
	assert.Empty(t, info.LeadingSystemPrompt(false), "a second stage must not reapply the prompts")
	assert.Empty(t, info.LeadingSystemPrompt(true))
}

func TestSystemPromptPrefixDoesNotConsumePromptState(t *testing.T) {
	info := &RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "model-two",
		RouteInjectPrompt:    "You are model-one.",
	}
	info.ChannelMeta = &ChannelMeta{ChannelSetting: dto.ChannelSettings{
		SystemPrompt:         "Channel policy.",
		SystemPromptOverride: true,
	}}

	const expected = "You are model-one.\nChannel policy."
	assert.Equal(t, expected, info.SystemPromptPrefix(true))
	assert.Equal(t, expected, info.SystemPromptPrefix(true))
	assert.Equal(t, expected, info.LeadingSystemPrompt(true))
	assert.Empty(t, info.LeadingSystemPrompt(true))
}

func TestLeadingSystemPromptAppliesAgainOnChannelRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &RelayInfo{
		UserModelRouteId:     7,
		RouteTargetModelName: "model-two",
		RouteInjectPrompt:    "You are model-one.",
	}

	info.InitChannelMeta(ctx)
	assert.Equal(t, "You are model-one.", info.LeadingSystemPrompt(false))
	assert.Empty(t, info.LeadingSystemPrompt(false), "one attempt must not inject twice")

	info.InitChannelMeta(ctx)
	assert.Equal(t, "You are model-one.", info.LeadingSystemPrompt(false), "a retry rebuilds the outbound request and must inject again")
}

// A request with nothing configured must stay unguarded, so a later stage that
// does have a prompt to apply is still able to apply it.
func TestLeadingSystemPromptDoesNotGuardWhenNothingWasApplied(t *testing.T) {
	info := &RelayInfo{}
	info.ChannelMeta = &ChannelMeta{}

	assert.Empty(t, info.LeadingSystemPrompt(false))

	info.ChannelSetting.SystemPrompt = "Channel policy."
	assert.Equal(t, "Channel policy.", info.LeadingSystemPrompt(false))
}

func TestJoinSystemPromptsSkipsBlankLayers(t *testing.T) {
	assert.Equal(t, "first\nsecond", JoinSystemPrompts("first", "second"))
	assert.Equal(t, "only", JoinSystemPrompts("", "  ", "only"))
	assert.Empty(t, JoinSystemPrompts("", "   "))
}
