package aws

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/pkg/errors"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	AwsClient  *bedrockruntime.Client
	AwsModelId string
	AwsReq     any
	IsNova     bool
}

func parseBedrockAPIKey(value string) (apiKey string, region string, err error) {
	parts := strings.SplitN(value, "|", 2)
	if len(parts) != 2 {
		return "", "", errors.New("invalid aws api key, should be in format of <api-key>|<region>")
	}
	apiKey = strings.TrimSpace(parts[0])
	region = strings.TrimSpace(parts[1])
	if apiKey == "" || region == "" {
		return "", "", errors.New("invalid aws api key, api key and region are required")
	}
	return apiKey, region, nil
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	for i, message := range request.Messages {
		updated := false
		if !message.IsStringContent() {
			content, err := message.ParseContent()
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse message content")
			}
			for i2, mediaMessage := range content {
				if mediaMessage.Source != nil {
					if mediaMessage.Source.Type == "url" {
						// 使用统一的文件服务获取图片数据
						source := types.NewURLFileSource(mediaMessage.Source.Url)
						base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Claude")
						if err != nil {
							return nil, fmt.Errorf("get file base64 from url failed: %s", err.Error())
						}
						mediaMessage.Source.MediaType = mimeType
						mediaMessage.Source.Data = base64Data
						mediaMessage.Source.Url = ""
						mediaMessage.Source.Type = "base64"
						content[i2] = mediaMessage
						updated = true
					}
				}
			}
			if updated {
				message.SetContent(content)
			}
		}
		if updated {
			request.Messages[i] = message
		}
	}
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.ChannelOtherSettings.AwsKeyType == dto.AwsKeyTypeApiKey {
		_, _, err := parseBedrockAPIKey(info.ApiKey)
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	claude.CommonClaudeHeadersOperation(c, req, info)
	if info.ChannelOtherSettings.AwsKeyType == dto.AwsKeyTypeApiKey {
		_, _, err := parseBedrockAPIKey(info.ApiKey)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	// 检查是否为Nova模型
	if isNovaModel(request.Model) {
		novaReq, err := convertToNovaRequest(request)
		if err != nil {
			return nil, err
		}
		a.IsNova = true
		return novaReq, nil
	}

	// 原有的Claude模型处理逻辑
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert openai request to claude request")
	}
	claudeReq, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("expected Anthropic Messages request, got %T", result.Value)
	}
	info.UpstreamModelName = claudeReq.Model
	return claudeReq, err
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, &request)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert OpenAI Responses request to Claude request")
	}
	claudeRequest, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("expected Anthropic Messages request, got %T", result.Value)
	}
	return a.ConvertClaudeRequest(c, info, claudeRequest)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return doAwsClientRequest(c, info, a, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if a.IsNova {
		err, usage = handleNovaRequest(c, info, a)
	} else {
		if info.IsStream {
			err, usage = awsStreamHandler(c, info, a)
		} else {
			err, usage = awsHandler(c, info, a)
		}
	}
	return
}

func (a *Adaptor) GetModelList() (models []string) {
	for n := range awsModelIDMap {
		models = append(models, n)
	}

	return
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
