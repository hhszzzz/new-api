package dify

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func uploadDifyFile(c *gin.Context, info *relaycommon.RelayInfo, user string, media dto.MediaContent) *DifyFile {
	uploadUrl := fmt.Sprintf("%s/v1/files/upload", info.ChannelBaseUrl)
	switch media.Type {
	case dto.ContentTypeImageURL:
		// Decode base64 data
		imageMedia := media.GetImageMedia()
		base64Data := imageMedia.Url
		// Remove base64 prefix if exists (e.g., "data:image/jpeg;base64,")
		if idx := strings.Index(base64Data, ","); idx != -1 {
			base64Data = base64Data[idx+1:]
		}

		// Decode base64 string
		decodedData, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			common.SysLog("failed to decode base64: " + err.Error())
			return nil
		}

		// Create temporary file
		tempFile, err := os.CreateTemp("", "dify-upload-*")
		if err != nil {
			common.SysLog("failed to create temp file: " + err.Error())
			return nil
		}
		defer tempFile.Close()
		defer os.Remove(tempFile.Name())

		// Write decoded data to temp file
		if _, err := tempFile.Write(decodedData); err != nil {
			common.SysLog("failed to write to temp file: " + err.Error())
			return nil
		}

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add user field
		if err := writer.WriteField("user", user); err != nil {
			common.SysLog("failed to add user field: " + err.Error())
			return nil
		}

		// Create form file with proper mime type
		mimeType := imageMedia.MimeType
		if mimeType == "" {
			mimeType = "image/jpeg" // default mime type
		}

		// Create form file
		part, err := writer.CreateFormFile("file", fmt.Sprintf("image.%s", strings.TrimPrefix(mimeType, "image/")))
		if err != nil {
			common.SysLog("failed to create form file: " + err.Error())
			return nil
		}

		// Copy file content to form
		if _, err = io.Copy(part, bytes.NewReader(decodedData)); err != nil {
			common.SysLog("failed to copy file content: " + err.Error())
			return nil
		}
		writer.Close()

		// Create HTTP request
		req, err := http.NewRequest("POST", uploadUrl, body)
		if err != nil {
			common.SysLog("failed to create request: " + err.Error())
			return nil
		}

		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", info.ApiKey))

		// Send request
		client := service.GetHttpClient()
		resp, err := client.Do(req)
		if err != nil {
			common.SysLog("failed to send request: " + err.Error())
			return nil
		}
		defer resp.Body.Close()

		// Parse response
		var result struct {
			Id string `json:"id"`
		}
		if err := common.DecodeJson(resp.Body, &result); err != nil {
			common.SysLog("failed to decode response: " + err.Error())
			return nil
		}

		return &DifyFile{
			UploadFileId: result.Id,
			Type:         "image",
			TransferMode: "local_file",
		}
	}
	return nil
}

func requestOpenAI2Dify(c *gin.Context, info *relaycommon.RelayInfo, request dto.GeneralOpenAIRequest) *DifyChatRequest {
	difyReq := DifyChatRequest{
		Inputs:           make(map[string]interface{}),
		AutoGenerateName: false,
	}

	user := request.User
	if len(user) == 0 {
		user = json.RawMessage(helper.GetResponseID(c))
	}
	var stringUser string
	err := common.Unmarshal(user, &stringUser)
	if err != nil {
		common.SysLog("failed to unmarshal user: " + err.Error())
		stringUser = helper.GetResponseID(c)
	}
	difyReq.User = stringUser

	files := make([]DifyFile, 0)
	var content strings.Builder
	for _, message := range request.Messages {
		if message.Role == "system" {
			content.WriteString("SYSTEM: \n" + message.StringContent() + "\n")
		} else if message.Role == "assistant" {
			content.WriteString("ASSISTANT: \n" + message.StringContent() + "\n")
		} else {
			parseContent := message.ParseContent()
			for _, mediaContent := range parseContent {
				switch mediaContent.Type {
				case dto.ContentTypeText:
					content.WriteString("USER: \n" + mediaContent.Text + "\n")
				case dto.ContentTypeImageURL:
					media := mediaContent.GetImageMedia()
					var file *DifyFile
					if media.IsRemoteImage() {
						// 修复 #2083: 远程图片分支此前未初始化 file，
						// 导致 file.Type = ... 触发 nil pointer dereference
						// 而 panic（500: "invalid memory address or nil pointer dereference"）。
						file = &DifyFile{
							Type:         media.MimeType,
							TransferMode: "remote_url",
							URL:          media.Url,
						}
					} else {
						file = uploadDifyFile(c, info, difyReq.User, mediaContent)
					}
					if file != nil {
						files = append(files, *file)
					}
				}
			}
		}
	}
	difyReq.Query = content.String()
	difyReq.Files = files
	mode := "blocking"
	if lo.FromPtrOr(request.Stream, false) {
		mode = "streaming"
	}
	difyReq.ResponseMode = mode
	return &difyReq
}

func streamResponseDify2OpenAI(difyResponse DifyChunkChatCompletionResponse) *dto.ChatCompletionsStreamResponse {
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "dify",
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if strings.HasPrefix(difyResponse.Event, "workflow_") {
		if constant.DifyDebug {
			text := "Workflow: " + difyResponse.Data.WorkflowId
			if difyResponse.Event == "workflow_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if strings.HasPrefix(difyResponse.Event, "node_") {
		if constant.DifyDebug {
			text := "Node: " + difyResponse.Data.NodeType
			if difyResponse.Event == "node_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if difyResponse.Event == "message" || difyResponse.Event == "agent_message" {
		if difyResponse.Answer == "<details style=\"color:gray;background-color: #f8f8f8;padding: 8px;border-radius: 4px;\" open> <summary> Thinking... </summary>\n" {
			difyResponse.Answer = "<think>"
		} else if difyResponse.Answer == "</details>" {
			difyResponse.Answer = "</think>"
		}

		choice.Delta.SetContentString(difyResponse.Answer)
	}
	response.Choices = append(response.Choices, choice)
	return &response
}

func difyRequiresSuccessfulWorkflow(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelOtherSettings.DifyRequireSuccessfulWorkflow
}

func newDifyResponseError(code types.ErrorCode, message string) *types.NewAPIError {
	return types.NewOpenAIError(errors.New(message), code, http.StatusBadGateway)
}

func difyErrorStatus(status any) bool {
	switch value := status.(type) {
	case nil:
		return false
	case float64:
		return value < 200 || value >= 300
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" || normalized == "ok" || normalized == "success" || normalized == "succeeded" {
			return false
		}
		if statusCode, err := strconv.Atoi(normalized); err == nil {
			return statusCode < 200 || statusCode >= 300
		}
		return true
	default:
		return true
	}
}

func difyStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var responseText strings.Builder
	usage := &dto.Usage{}
	var nodeToken int
	var streamErr *types.NewAPIError
	var messageEndSeen bool
	var workflowFinishedSeen bool
	var workflowTotalTokens int
	strictWorkflow := difyRequiresSuccessfulWorkflow(info)
	helper.SetEventStreamHeaders(c)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var difyResponse DifyChunkChatCompletionResponse
		if err := common.Unmarshal([]byte(data), &difyResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			streamErr = newDifyResponseError(types.ErrorCodeBadResponse, "invalid Dify stream event")
			sr.Stop(err)
			return
		}
		if difyResponse.WorkflowRunId != "" {
			info.DifyWorkflowRunID = difyResponse.WorkflowRunId
		} else if strings.HasPrefix(difyResponse.Event, "workflow_") && difyResponse.Data.Id != "" {
			info.DifyWorkflowRunID = difyResponse.Data.Id
		}
		switch difyResponse.Event {
		case "workflow_finished":
			workflowFinishedSeen = true
			workflowTotalTokens = difyResponse.Data.TotalTokens
			info.DifyWorkflowStatus = strings.ToLower(strings.TrimSpace(difyResponse.Data.Status))
			if strictWorkflow && info.DifyWorkflowStatus != "succeeded" {
				streamErr = newDifyResponseError(
					types.ErrorCodeBadResponse,
					fmt.Sprintf("Dify workflow finished with status %q", info.DifyWorkflowStatus),
				)
				sr.Stop(streamErr)
				return
			}
		case "message_end":
			usage = &difyResponse.MetaData.Usage
			messageEndSeen = true
			sr.Done()
			return
		case "error":
			if info.DifyWorkflowStatus == "" {
				info.DifyWorkflowStatus = "error"
			}
			message := strings.TrimSpace(difyResponse.Message)
			if message == "" {
				message = strings.TrimSpace(difyResponse.Data.Error)
			}
			if message == "" {
				message = "Dify returned an error event"
			}
			streamErr = newDifyResponseError(types.ErrorCodeBadResponse, message)
			sr.Stop(streamErr)
			return
		}
		openaiResponse := *streamResponseDify2OpenAI(difyResponse)
		if len(openaiResponse.Choices) != 0 {
			responseText.WriteString(openaiResponse.Choices[0].Delta.GetContentString())
			if openaiResponse.Choices[0].Delta.ReasoningContent != nil {
				nodeToken += 1
			}
		}
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			streamErr = newDifyResponseError(types.ErrorCodeBadResponse, "failed to write Dify stream response")
			sr.Stop(err)
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if !messageEndSeen {
		return nil, newDifyResponseError(types.ErrorCodeBadResponse, "Dify stream ended without message_end")
	}
	if info.StreamStatus == nil || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() {
		return nil, newDifyResponseError(types.ErrorCodeBadResponse, "Dify stream terminated abnormally")
	}
	if strictWorkflow {
		if !workflowFinishedSeen {
			return nil, newDifyResponseError(types.ErrorCodeBadResponse, "Dify stream ended without workflow_finished")
		}
		if strings.TrimSpace(responseText.String()) == "" {
			return nil, newDifyResponseError(types.ErrorCodeEmptyResponse, "Dify workflow returned an empty response")
		}
		if workflowTotalTokens <= 0 || usage.TotalTokens <= 0 {
			return nil, newDifyResponseError(types.ErrorCodeBadResponse, "Dify workflow returned zero token usage")
		}
	}
	if err := helper.Done(c); err != nil {
		return nil, newDifyResponseError(types.ErrorCodeBadResponse, "failed to finish Dify stream response")
	}
	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	usage.CompletionTokens += nodeToken
	return usage, nil
}

func difyHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var difyResponse DifyChatCompletionResponse
	responseBody, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	err = common.Unmarshal(responseBody, &difyResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if strings.EqualFold(difyResponse.Event, "error") || difyResponse.Code != "" || difyErrorStatus(difyResponse.Status) ||
		(difyResponse.Message != "" && difyResponse.ConversationId == "" && difyResponse.Answer == "") {
		message := strings.TrimSpace(difyResponse.Message)
		if message == "" {
			message = "Dify returned an error response"
		}
		return nil, newDifyResponseError(types.ErrorCodeBadResponse, message)
	}
	if difyRequiresSuccessfulWorkflow(info) {
		if strings.TrimSpace(difyResponse.Answer) == "" {
			return nil, newDifyResponseError(types.ErrorCodeEmptyResponse, "Dify workflow returned an empty response")
		}
		if difyResponse.MetaData.Usage.TotalTokens <= 0 {
			return nil, newDifyResponseError(types.ErrorCodeBadResponse, "Dify workflow returned zero token usage")
		}
	}
	fullTextResponse := dto.OpenAITextResponse{
		Id:      difyResponse.ConversationId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Usage:   difyResponse.MetaData.Usage,
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: difyResponse.Answer,
		},
		FinishReason: "stop",
	}
	fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if err := service.IOCopyBytesGracefully(c, resp, jsonResponse); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	return &difyResponse.MetaData.Usage, nil
}
