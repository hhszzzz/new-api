package setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var chats = []map[string]string{
	//{
	//	"ChatGPT Next Web 官方示例": "https://app.nextchat.dev/#/?settings={\"key\":\"{key}\",\"url\":\"{address}\"}",
	//},
	{
		"Cherry Studio": "cherrystudio://providers/api-keys?v=1&data={cherryConfig}",
	},
	{
		"AionUI": "aionui://provider/add?v=1&data={aionuiConfig}",
	},
	{
		"流畅阅读": "fluentread",
	},
	{
		"CC Switch": "ccswitch",
	},
	{
		"DeepChat": "deepchat://provider/install?v=1&data={deepchatConfig}",
	},
	{
		"Lobe Chat 官方示例": "https://chat-preview.lobehub.com/?settings={\"keyVaults\":{\"openai\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\"}}}",
	},
	{
		"AI as Workspace": "https://aiaw.app/set-provider?provider={\"type\":\"openai\",\"settings\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\",\"compatibility\":\"strict\"}}",
	},
	{
		"AMA 问天": "ama://set-api-key?server={address}&key={key}",
	},
	{
		"OpenCat": "opencat://team/join?domain={address}&token={key}",
	},
}
var chatsMutex sync.RWMutex

func UpdateChatsByJsonString(jsonString string) error {
	var parsed []map[string]string
	if err := common.UnmarshalJsonStr(jsonString, &parsed); err != nil {
		return err
	}
	chatsMutex.Lock()
	chats = parsed
	chatsMutex.Unlock()
	return nil
}

func Chats2JsonString() string {
	jsonBytes, err := common.Marshal(GetChats())
	if err != nil {
		common.SysLog("error marshalling chats: " + err.Error())
		return "[]"
	}
	return string(jsonBytes)
}

func GetChats() []map[string]string {
	chatsMutex.RLock()
	defer chatsMutex.RUnlock()

	result := make([]map[string]string, len(chats))
	for index, chat := range chats {
		result[index] = make(map[string]string, len(chat))
		for key, value := range chat {
			result[index][key] = value
		}
	}
	return result
}
