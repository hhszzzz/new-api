package openai

import (
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyRealtimeSessionPrompt composes configured prompt layers into client
// session.update events without rebuilding the event from the partial DTO.
// Realtime events are extensible, so preserving unknown fields is part of the
// protocol contract.
func applyRealtimeSessionPrompt(message []byte, info *relaycommon.RelayInfo, promptApplied *bool) ([]byte, error) {
	if info == nil || promptApplied == nil {
		return message, nil
	}
	eventType := gjson.GetBytes(message, "type")
	if eventType.Type != gjson.String || eventType.String() != dto.RealtimeEventTypeSessionUpdate {
		return message, nil
	}
	session := gjson.GetBytes(message, "session")
	if !session.IsObject() {
		return message, nil
	}

	instructions := session.Get("instructions")
	hasExplicitInstructions := instructions.Raw != ""
	if hasExplicitInstructions && instructions.Type != gjson.String && instructions.Raw != "null" {
		return message, nil
	}
	if !hasExplicitInstructions && *promptApplied {
		return message, nil
	}

	clientPrompt := ""
	if instructions.Type == gjson.String {
		clientPrompt = strings.TrimSpace(instructions.String())
	}
	leadingPrompt := info.SystemPromptPrefix(clientPrompt != "")
	if leadingPrompt == "" {
		// A configured non-overriding channel prompt is intentionally skipped
		// when the client supplied instructions. Still consume the initial
		// update so a later voice-only update cannot replace those instructions.
		if info.SystemPromptPrefix(false) != "" {
			*promptApplied = true
		}
		return message, nil
	}
	if clientPrompt != "" {
		leadingPrompt += "\n" + clientPrompt
	}

	updated, err := sjson.SetBytes(message, "session.instructions", leadingPrompt)
	if err != nil {
		return nil, err
	}
	*promptApplied = true
	return updated, nil
}

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry()), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	errChan := make(chan error, 4)
	var handlerErr error

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	var usageMu sync.Mutex
	var readers sync.WaitGroup
	promptApplied := false
	readers.Add(2)

	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in client reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from client: %v", err)
						return
					}
					close(clientClosed)
					return
				}

				upstreamMessage, err := applyRealtimeSessionPrompt(message, info, &promptApplied)
				if err != nil {
					errChan <- fmt.Errorf("error applying realtime system prompt: %v", err)
					return
				}
				if info.HasUserModelRoute() {
					upstreamMessage, err = relaycommon.RewriteUserModelRouteRequestJSON(upstreamMessage, info.SelectionModelName())
					if err != nil {
						errChan <- fmt.Errorf("error rewriting routed model: %v", err)
						return
					}
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(upstreamMessage, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				err = func() error {
					usageMu.Lock()
					defer usageMu.Unlock()

					if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
						if realtimeEvent.Session != nil {
							if realtimeEvent.Session.Tools != nil {
								info.RealtimeTools = realtimeEvent.Session.Tools
							}
						}
					}

					textToken, audioToken, countErr := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if countErr != nil {
						return fmt.Errorf("error counting text token: %w", countErr)
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					localUsage.InputTokens += textToken + audioToken
					localUsage.InputTokenDetails.TextTokens += textToken
					localUsage.InputTokenDetails.AudioTokens += audioToken
					return nil
				}()
				if err != nil {
					errChan <- err
					return
				}

				err = helper.WssString(c, targetConn, string(upstreamMessage))
				if err != nil {
					errChan <- fmt.Errorf("error writing to target: %v", err)
					return
				}

			}
		}
	})

	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in target reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from target: %v", err)
						return
					}
					close(targetClosed)
					return
				}
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				err = func() error {
					usageMu.Lock()
					defer usageMu.Unlock()

					if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
						if realtimeEvent.Response == nil {
							return fmt.Errorf("response.done event is missing response")
						}
						realtimeUsage := realtimeEvent.Response.Usage
						if realtimeUsage != nil {
							usage.TotalTokens += realtimeUsage.TotalTokens
							usage.InputTokens += realtimeUsage.InputTokens
							usage.OutputTokens += realtimeUsage.OutputTokens
							usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
							usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
							usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
							usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
							usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
							if consumeErr := preConsumeUsage(c, info, usage, sumUsage); consumeErr != nil {
								return types.NewError(fmt.Errorf("error consume usage: %w", consumeErr), types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry())
							}
							usage = &dto.RealtimeUsage{}
							localUsage = &dto.RealtimeUsage{}
						} else {
							textToken, audioToken, countErr := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
							if countErr != nil {
								return fmt.Errorf("error counting text token: %w", countErr)
							}
							logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
							localUsage.TotalTokens += textToken + audioToken
							info.IsFirstRequest = false
							localUsage.InputTokens += textToken + audioToken
							localUsage.InputTokenDetails.TextTokens += textToken
							localUsage.InputTokenDetails.AudioTokens += audioToken
							if consumeErr := preConsumeUsage(c, info, localUsage, sumUsage); consumeErr != nil {
								return types.NewError(fmt.Errorf("error consume usage: %w", consumeErr), types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry())
							}
							localUsage = &dto.RealtimeUsage{}
						}
						logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
						logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))
					} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
						realtimeSession := realtimeEvent.Session
						if realtimeSession != nil {
							info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
							info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
						}
					} else {
						textToken, audioToken, countErr := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if countErr != nil {
							return fmt.Errorf("error counting text token: %w", countErr)
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsage.TotalTokens += textToken + audioToken
						localUsage.OutputTokens += textToken + audioToken
						localUsage.OutputTokenDetails.TextTokens += textToken
						localUsage.OutputTokenDetails.AudioTokens += audioToken
					}
					return nil
				}()
				if err != nil {
					errChan <- err
					return
				}

				clientMessage := message
				if info.HasUserModelRoute() {
					clientMessage, err = relaycommon.RedactUserModelRouteJSON(message, info)
					if err != nil {
						errChan <- fmt.Errorf("error redacting routed model: %v", err)
						return
					}
				}
				err = helper.WssString(c, clientConn, string(clientMessage))
				if err != nil {
					errChan <- fmt.Errorf("error writing to client: %v", err)
					return
				}

			}
		}
	})

	select {
	case <-clientClosed:
	case <-targetClosed:
	case err := <-errChan:
		handlerErr = err
		logger.LogError(c, "realtime error: "+err.Error())
	case <-c.Done():
	}

	_ = clientConn.Close()
	_ = targetConn.Close()
	readers.Wait()

	if handlerErr != nil {
		return types.NewError(handlerErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry()), nil
	}

	if usage.TotalTokens != 0 {
		if err := preConsumeUsage(c, info, usage, sumUsage); err != nil {
			return types.NewError(fmt.Errorf("error consume final upstream usage: %w", err), types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry()), nil
		}
	}

	if localUsage.TotalTokens != 0 {
		if err := preConsumeUsage(c, info, localUsage, sumUsage); err != nil {
			return types.NewError(fmt.Errorf("error consume final local usage: %w", err), types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry()), nil
		}
	}
	finalUsage := *sumUsage

	return nil, &finalUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	nextTotal := *totalUsage
	nextTotal.TotalTokens += usage.TotalTokens
	nextTotal.InputTokens += usage.InputTokens
	nextTotal.OutputTokens += usage.OutputTokens
	nextTotal.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	nextTotal.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	nextTotal.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	nextTotal.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	nextTotal.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	if err := service.PreWssConsumeQuota(ctx, info, &nextTotal); err != nil {
		return err
	}
	*totalUsage = nextTotal
	return nil
}
