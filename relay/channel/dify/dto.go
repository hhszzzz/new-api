package dify

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type DifyChatRequest struct {
	Inputs           map[string]interface{} `json:"inputs"`
	Query            string                 `json:"query"`
	ResponseMode     string                 `json:"response_mode"`
	User             string                 `json:"user"`
	AutoGenerateName bool                   `json:"auto_generate_name"`
	Files            []DifyFile             `json:"files"`
}

type DifyFile struct {
	Type         string `json:"type"`
	TransferMode string `json:"transfer_mode"`
	URL          string `json:"url,omitempty"`
	UploadFileId string `json:"upload_file_id,omitempty"`
}

type DifyMetaData struct {
	Usage dto.Usage `json:"usage"`
}

type DifyData struct {
	Id          string `json:"id"`
	WorkflowId  string `json:"workflow_id"`
	NodeId      string `json:"node_id"`
	NodeType    string `json:"node_type"`
	Status      string `json:"status"`
	Error       string `json:"error"`
	TotalTokens int    `json:"total_tokens"`
}

type DifyChatCompletionResponse struct {
	Event          string       `json:"event"`
	ConversationId string       `json:"conversation_id"`
	Answer         string       `json:"answer"`
	CreateAt       int64        `json:"create_at"`
	MetaData       DifyMetaData `json:"metadata"`
	Code           string       `json:"code"`
	Message        string       `json:"message"`
	Status         any          `json:"status"`
}

type DifyChunkChatCompletionResponse struct {
	Event          string       `json:"event"`
	WorkflowRunId  string       `json:"workflow_run_id"`
	ConversationId string       `json:"conversation_id"`
	Answer         string       `json:"answer"`
	Data           DifyData     `json:"data"`
	MetaData       DifyMetaData `json:"metadata"`
	Code           string       `json:"code"`
	Message        string       `json:"message"`
	Status         any          `json:"status"`
}
