package relayconvert

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// Protocol describes wire semantics. Operations and transports are independent:
// a Responses compact request is not a generation request over another transport.
type Protocol string

const (
	ProtocolChat      Protocol = "chat"
	ProtocolMessages  Protocol = "messages"
	ProtocolResponses Protocol = "responses"
	ProtocolGemini    Protocol = "gemini"
)

type Operation string

const (
	OperationGenerate           Operation = "generate"
	OperationCompletions        Operation = "completions"
	OperationModeration         Operation = "moderation"
	OperationCompact            Operation = "compact"
	OperationCountTokens        Operation = "count_tokens"
	OperationImage              Operation = "image"
	OperationImageEdit          Operation = "image_edit"
	OperationAudioSpeech        Operation = "audio_speech"
	OperationAudioTranscription Operation = "audio_transcription"
	OperationAudioTranslation   Operation = "audio_translation"
	OperationEmbedding          Operation = "embedding"
	OperationRerank             Operation = "rerank"
	OperationRealtime           Operation = "realtime"
	OperationAlphaSearch        Operation = "alpha_search"
	OperationTask               Operation = "task"
	OperationModels             Operation = "models"
	OperationBalance            Operation = "balance"
)

type Transport string

const (
	TransportHTTP      Transport = "http"
	TransportSSE       Transport = "sse"
	TransportWebSocket Transport = "websocket"
	TransportSDK       Transport = "sdk_events"
)

type ProtocolSpec struct {
	ID     Protocol          `json:"id"`
	Format types.RelayFormat `json:"format"`
	Name   string            `json:"name"`
}

type OperationSpec struct {
	ID          Operation   `json:"id"`
	Protocol    Protocol    `json:"protocol,omitempty"`
	Path        string      `json:"path"`
	Transports  []Transport `json:"transports"`
	Convertible bool        `json:"convertible"`
}

type ConversionSpec struct {
	ID            string   `json:"id"`
	Aliases       []string `json:"aliases,omitempty"`
	From          Protocol `json:"from"`
	To            Protocol `json:"to"`
	RequestSteps  []string `json:"request_steps"`
	ResponseSteps []string `json:"response_steps"`
}

type ProtocolCatalog struct {
	Version     int              `json:"version"`
	Protocols   []ProtocolSpec   `json:"protocols"`
	Operations  []OperationSpec  `json:"operations"`
	Conversions []ConversionSpec `json:"conversions"`
}

var protocolSpecs = []ProtocolSpec{
	{ProtocolChat, types.RelayFormatOpenAI, "OpenAI Chat Completions"},
	{ProtocolMessages, types.RelayFormatClaude, "Anthropic Messages"},
	{ProtocolResponses, types.RelayFormatOpenAIResponses, "OpenAI Responses"},
	{ProtocolGemini, types.RelayFormatGemini, "Gemini GenerateContent"},
}

var operationSpecs = []OperationSpec{
	{OperationGenerate, ProtocolChat, "/v1/chat/completions", []Transport{TransportHTTP, TransportSSE, TransportSDK}, true},
	{OperationGenerate, ProtocolMessages, "/v1/messages", []Transport{TransportHTTP, TransportSSE, TransportSDK}, true},
	{OperationGenerate, ProtocolResponses, "/v1/responses", []Transport{TransportHTTP, TransportSSE, TransportWebSocket}, true},
	{OperationGenerate, ProtocolGemini, "/v1beta/models/{model}:generateContent", []Transport{TransportHTTP, TransportSSE, TransportSDK}, true},
	{OperationCompact, ProtocolResponses, "/v1/responses/compact", []Transport{TransportHTTP}, true},
	{OperationCountTokens, ProtocolMessages, "/v1/messages/count_tokens", []Transport{TransportHTTP, TransportSDK}, false},
	{OperationImage, ProtocolChat, "/v1/images/generations", []Transport{TransportHTTP, TransportSSE}, false},
	{OperationImageEdit, ProtocolChat, "/v1/images/edits", []Transport{TransportHTTP, TransportSSE}, false},
	{OperationAudioSpeech, ProtocolChat, "/v1/audio/speech", []Transport{TransportHTTP, TransportSSE}, false},
	{OperationAudioTranscription, ProtocolChat, "/v1/audio/transcriptions", []Transport{TransportHTTP, TransportSSE}, false},
	{OperationAudioTranslation, ProtocolChat, "/v1/audio/translations", []Transport{TransportHTTP}, false},
	{OperationEmbedding, ProtocolChat, "/v1/embeddings", []Transport{TransportHTTP}, false},
	{OperationRerank, ProtocolChat, "/v1/rerank", []Transport{TransportHTTP}, false},
	{OperationRealtime, ProtocolChat, "/v1/realtime", []Transport{TransportWebSocket}, false},
	{OperationAlphaSearch, ProtocolChat, "/v1/alpha/search", []Transport{TransportHTTP}, false},
	{OperationTask, "", "/v1/tasks", []Transport{TransportHTTP}, false},
	{OperationTask, "", "/v1/videos", []Transport{TransportHTTP}, false},
	{OperationModels, "", "/v1/models", []Transport{TransportHTTP}, false},
	{OperationBalance, "", "/v1/dashboard/billing/credit_grants", []Transport{TransportHTTP}, false},
	{OperationCompletions, ProtocolChat, "/v1/completions", []Transport{TransportHTTP, TransportSSE}, false},
	{OperationModeration, ProtocolChat, "/v1/moderations", []Transport{TransportHTTP}, false},
	{OperationEmbedding, ProtocolGemini, "/v1beta/models/{model}:embedContent", []Transport{TransportHTTP}, false},
	{OperationEmbedding, ProtocolGemini, "/v1beta/models/{model}:batchEmbedContents", []Transport{TransportHTTP}, false},
}

func (p Protocol) RelayFormat() (types.RelayFormat, bool) {
	for _, spec := range protocolSpecs {
		if spec.ID == p {
			return spec.Format, true
		}
	}
	return "", false
}

func ProtocolForFormat(format types.RelayFormat) Protocol {
	for _, spec := range protocolSpecs {
		if spec.Format == format {
			return spec.ID
		}
	}
	return ""
}

func Protocols() []Protocol {
	result := make([]Protocol, len(protocolSpecs))
	for i, spec := range protocolSpecs {
		result[i] = spec.ID
	}
	return result
}

// Catalog is a detached snapshot of the same declarations used to execute
// conversions. Configuration validation and management UIs must use this data.
func Catalog() ProtocolCatalog {
	result := ProtocolCatalog{Version: 1, Protocols: slices.Clone(protocolSpecs), Conversions: []ConversionSpec{}}
	for _, operation := range operationSpecs {
		operation.Transports = slices.Clone(operation.Transports)
		result.Operations = append(result.Operations, operation)
	}
	for _, converter := range builtinTextConverters {
		reverse, ok := LookupTextConverterRoute(converter.To, converter.From)
		if !ok {
			continue
		}
		requestSteps := slices.Clone(converter.Req.StepConverters)
		if len(requestSteps) == 0 {
			requestSteps = []string{converter.ID}
		}
		responseSteps := slices.Clone(reverse.Resp.StepConverters)
		if len(responseSteps) == 0 {
			responseSteps = []string{reverse.ID}
		}
		result.Conversions = append(result.Conversions, ConversionSpec{ID: converter.ID, Aliases: slices.Clone(converter.Resp.Aliases), From: ProtocolForFormat(converter.From), To: ProtocolForFormat(converter.To), RequestSteps: requestSteps, ResponseSteps: responseSteps})
	}
	return result
}

// DetectOperation matches specific operations before their generation prefix.
// Unknown paths are not classified as text generation.
func DetectOperation(path string) (OperationSpec, bool) {
	path, _, _ = strings.Cut(strings.TrimSpace(path), "?")
	if path == "/pg/chat/completions" {
		path = "/v1/chat/completions"
	}
	for _, spec := range operationSpecs {
		if path == spec.Path {
			return spec, true
		}
		if prefix, suffix, templated := strings.Cut(spec.Path, "{model}"); templated && strings.HasPrefix(path, prefix) && strings.HasSuffix(path, suffix) {
			return spec, true
		}
	}
	if strings.Contains(path, "/models/") && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")) {
		return operationSpecs[3], true
	}
	if strings.HasPrefix(path, "/v1/tasks/") || strings.HasPrefix(path, "/v1/videos/") {
		return operationSpecs[15], true
	}
	return OperationSpec{}, false
}

func CanonicalPath(protocol Protocol, model string) string {
	for _, spec := range operationSpecs {
		if spec.ID == OperationGenerate && spec.Protocol == protocol {
			if strings.TrimSpace(model) == "" {
				model = "model"
			}
			return strings.ReplaceAll(spec.Path, "{model}", model)
		}
	}
	return ""
}

// ResolveTarget accepts legacy converter IDs only at the configuration boundary.
// Every execution plan records protocols and resolves both conversion directions.
func ResolveTarget(incomingPath, target, legacyConverter string) (Protocol, error) {
	source, known := DetectOperation(incomingPath)
	if target == "native" {
		if legacyConverter != "" && legacyConverter != ConverterNone {
			return "", fmt.Errorf("native target conflicts with legacy converter")
		}
		target = ""
	}
	var legacyTarget Protocol
	if legacyConverter != "" && legacyConverter != ConverterNone {
		spec, ok := LookupTextConverter(legacyConverter)
		if !ok {
			return "", fmt.Errorf("converter %q is not registered", legacyConverter)
		}
		if !known || !source.Convertible || ProtocolForFormat(spec.From) != source.Protocol {
			return "", fmt.Errorf("converter does not match incoming_path: %q", legacyConverter)
		}
		legacyTarget = ProtocolForFormat(spec.To)
	}
	if target == "" {
		if legacyTarget != "" {
			return legacyTarget, nil
		}
		return source.Protocol, nil
	}
	protocol := Protocol(target)
	format, valid := protocol.RelayFormat()
	if !valid {
		return "", fmt.Errorf("unknown target protocol %q", target)
	}
	if !known || (!source.Convertible && protocol != source.Protocol) {
		return "", fmt.Errorf("operation does not support protocol conversion")
	}
	if legacyTarget != "" && legacyTarget != protocol {
		return "", fmt.Errorf("target_protocol conflicts with legacy converter")
	}
	if protocol != source.Protocol {
		from, _ := source.Protocol.RelayFormat()
		if _, ok := LookupTextConverterRoute(from, format); !ok {
			return "", fmt.Errorf("no conversion route from %s to %s", source.Protocol, protocol)
		}
	}
	return protocol, nil
}
