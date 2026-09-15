package relayconvert

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ConversionSession owns all mutable codec state for one upstream attempt.
// Construct a new session for a retry or a WebSocket logical request. A session
// is driven serially, just like the ordered protocol events it consumes.
type ConversionSession struct {
	meta    sessionMeta
	bridge  sharedbridge.State
	streams map[responseConverterRoute]*ResponseStreamState
	closed  bool
}

type sessionMeta struct {
	convmeta.Values
	observer convmeta.Meta
}

func (m *sessionMeta) AppendRequestConversion(format types.RelayFormat) {
	m.Values.AppendRequestConversion(format)
	if m.observer != nil {
		m.observer.AppendRequestConversion(format)
	}
}

func (m *sessionMeta) SetReasoningEffort(effort string) {
	m.Values.SetReasoningEffort(effort)
	if m.observer != nil {
		m.observer.SetReasoningEffort(effort)
	}
}

func NewConversionSession(meta convmeta.Meta) *ConversionSession {
	s := &ConversionSession{streams: make(map[responseConverterRoute]*ResponseStreamState)}
	s.meta.observer = meta
	options := *convmeta.OptionsOf(meta)
	s.meta.Options = &options
	if meta != nil {
		s.meta.OriginModelName = meta.GetOriginModelName()
		s.meta.UpstreamModelName = meta.GetUpstreamModelName()
		s.meta.ChannelMetaAttached = meta.HasChannelMeta()
		s.meta.ChannelID = meta.GetChannelID()
		s.meta.ChannelType = meta.GetChannelType()
		s.meta.IsStream = meta.GetIsStream()
		s.meta.ReasoningEffort = meta.GetReasoningEffort()
		s.meta.EstimatePromptTokens = meta.GetEstimatePromptTokens()
		if state := meta.ReasoningState(); state != nil {
			copy := *state
			s.meta.ReasoningConversion = &copy
		}
	}
	return s
}

type originalContextKey struct{}

func OriginalContext(ctx context.Context) context.Context {
	if isNilRequest(ctx) {
		return nil
	}
	if original, ok := ctx.Value(originalContextKey{}).(context.Context); ok {
		return original
	}
	return ctx
}

func (s *ConversionSession) context(ctx context.Context) context.Context {
	if isNilRequest(ctx) {
		ctx = context.Background()
	}
	return sharedbridge.WithState(context.WithValue(ctx, originalContextKey{}, OriginalContext(ctx)), &s.bridge)
}

func (s *ConversionSession) Request(ctx context.Context, target types.RelayFormat, request any) (*RequestResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	return ConvertRequest(s.context(ctx), &s.meta, target, request)
}

func (s *ConversionSession) RequestByID(ctx context.Context, converter string, request any) (*RequestResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	return ConvertRequestByID(s.context(ctx), &s.meta, converter, request)
}

func (s *ConversionSession) RequestVia(ctx context.Context, request any, path ...types.RelayFormat) (*RequestResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	return ConvertRequestVia(s.context(ctx), &s.meta, request, path...)
}

func (s *ConversionSession) Response(ctx context.Context, target types.RelayFormat, response any) (*ResponseResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	return ConvertResponse(s.context(ctx), &s.meta, target, response)
}

func (s *ConversionSession) ClaudeState() *convmeta.ClaudeConvertInfo {
	return s.meta.EnsureClaudeConvertInfo()
}

func (s *ConversionSession) StreamResponse(ctx context.Context, target types.RelayFormat, response any) (*ResponseResult, error) {
	from, err := inferResponseRelayFormat(response)
	if err != nil {
		return nil, err
	}
	state, err := s.StreamState(from, target, ResponseStreamOptions{})
	if err != nil {
		return nil, err
	}
	results, err := s.Stream(ctx, state, response)
	if err != nil {
		return nil, err
	}
	result := &ResponseResult{From: from, To: target, Stream: true, Usage: state.Usage(), Diagnostics: state.Diagnostics()}
	if target == types.RelayFormatClaude {
		values := make([]*dto.ClaudeResponse, 0, len(results))
		for _, item := range results {
			if value, ok := item.Value.(*dto.ClaudeResponse); ok {
				values = append(values, value)
			}
		}
		result.Value = values
	} else if len(results) == 1 {
		result.Value = results[0].Value
	} else {
		values := make([]any, len(results))
		for i := range results {
			values[i] = results[i].Value
		}
		result.Value = values
	}
	return result, nil
}

func (s *ConversionSession) StreamState(from, target types.RelayFormat, options ResponseStreamOptions) (*ResponseStreamState, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	key := responseConverterRoute{from: from, to: target}
	if state := s.streams[key]; state != nil {
		return state, nil
	}
	state, err := NewResponseStreamState(from, target, options)
	if err != nil {
		return nil, err
	}
	state.owner = s
	s.streams[key] = state
	return state, nil
}

func (s *ConversionSession) Stream(ctx context.Context, state *ResponseStreamState, response any) ([]ResponseResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	if state == nil {
		return nil, errors.New("response stream state is required")
	}
	if state.owner != nil && state.owner != s {
		return nil, errors.New("stream state belongs to a different conversion session")
	}
	state.owner = s
	key := responseConverterRoute{from: state.From, to: state.To}
	if existing := s.streams[key]; existing != nil && existing != state {
		return nil, errors.New("stream state belongs to a different conversion")
	}
	s.streams[key] = state
	return ConvertStreamResponseChunk(s.context(ctx), &s.meta, state, response)
}

func (s *ConversionSession) Finish(ctx context.Context, state *ResponseStreamState) ([]ResponseResult, error) {
	if s == nil || s.closed {
		return nil, errors.New("conversion session is closed")
	}
	if state == nil || state.owner != nil && state.owner != s {
		return nil, errors.New("stream state does not belong to this conversion session")
	}
	state.owner = s
	return FinalizeStreamResponse(s.context(ctx), &s.meta, state)
}

func (s *ConversionSession) Close() {
	if s == nil {
		return
	}
	s.closed = true
	s.streams = nil
	s.bridge = sharedbridge.State{}
	s.meta = sessionMeta{}
}
