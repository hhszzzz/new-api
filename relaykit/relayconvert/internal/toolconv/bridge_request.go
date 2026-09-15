package toolconv

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	sharedbridge "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/bridge"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type deferredRequestToolsKey struct{}

// WithDeferredRequestTools reserves tool rendering for the final route hop.
// The marker is immutable; mutable tool identities belong to ConversionSession.
func WithDeferredRequestTools(ctx context.Context) context.Context {
	return context.WithValue(ctx, deferredRequestToolsKey{}, true)
}

// RenderRequestTools gives standalone directional codecs the same semantic
// encoder as the registry. A planned route extracts once and renders after all
// content hops, so an intermediate protocol cannot discard a supported tool.
func RenderRequestTools(ctx context.Context, from, to types.RelayFormat, source, target any, options *convmeta.Options) error {
	ctx, _ = convdiag.WithCollector(ctx)
	if deferred, _ := ctx.Value(deferredRequestToolsKey{}).(bool); deferred {
		return nil
	}
	_, set, err := ExtractRequestForConversion(context.Background(), from, source)
	if err != nil {
		return err
	}
	_, diagnostics, err := AttachRequest(to, target, set, options)
	convdiag.Add(ctx, diagnostics...)
	return err
}

// ExtractRequestForConversion creates one semantic tool set. Reversible client
// tools and hosted tools are encoded together only after the final request hop.
func ExtractRequestForConversion(ctx context.Context, format types.RelayFormat, request any) (any, Set, error) {
	if format != types.RelayFormatOpenAIResponses {
		return ExtractRequest(format, request)
	}
	source, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok {
		value, valid := request.(dto.OpenAIResponsesRequest)
		if !valid {
			return nil, Set{}, fmt.Errorf("expected Responses request, got %T", request)
		}
		source = &value
	}
	declarations, err := CollectResponsesToolDeclarations(source)
	if err != nil {
		return nil, Set{}, err
	}
	state := sharedbridge.NewToolState()
	sharedbridge.SetToolState(ctx, state)
	set := Set{Source: format, ParallelAllowed: rawBoolPointer(source.ParallelToolCalls)}
	for _, tool := range declarations {
		kind := strings.TrimSpace(kitutil.Interface2String(tool["type"]))
		switch kind {
		case "", "function", "custom", "freeform", "namespace", "tool_search", "local_shell":
			functions, err := ResponsesToolToChatFunctions(tool, "", state)
			if err != nil {
				return nil, set, err
			}
			for _, function := range functions {
				identity, _ := state.ResolveUpstream(function.Function.Name)
				set.Definitions = append(set.Definitions, Definition{Kind: KindFunction, Execution: ExecutionClient, Name: function.Function.Name, Identity: &identity, Function: &Function{Name: function.Function.Name, Description: function.Function.Description, Parameters: function.Function.Parameters, Strict: function.Function.Strict}})
			}
		default:
			raw, err := kitutil.Marshal(tool)
			if err != nil {
				return nil, set, err
			}
			definition, err := decodeOpenAIResponsesDefinition(raw)
			if err != nil {
				return nil, set, err
			}
			set.Definitions = append(set.Definitions, definition)
		}
	}
	choice, err := ResponsesRequestToolChoiceToChat(source.ToolChoice, state)
	if err != nil {
		return nil, set, err
	}
	if object, ok := choice.(map[string]any); ok && object["type"] != "function" && object["type"] != "allowed_tools" {
		set.Choice, err = decodeOpenAIResponsesChoice(source.ToolChoice)
	} else {
		set.Choice, err = decodeOpenAIChatChoice(choice)
	}
	if err != nil {
		return nil, set, err
	}
	clone := *source
	clone.Tools = nil
	clone.ToolChoice = nil
	clone.ParallelToolCalls = nil
	clone.Input, set.History, err = extractOpenAIResponsesHostedHistory(source.Input)
	return &clone, set, err
}
