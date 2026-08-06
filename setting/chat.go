package setting

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const maxChatPresetGroups = 64

type ChatPreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type chatConfig struct {
	Name   string
	URL    string
	Groups []string
}

type restrictedChatConfig struct {
	URL    string   `json:"url"`
	Groups []string `json:"groups,omitempty"`
}

var chats = []chatConfig{
	//{
	//	Name: "ChatGPT Next Web 官方示例",
	//	URL:  "https://app.nextchat.dev/#/?settings={\"key\":\"{key}\",\"url\":\"{address}\"}",
	//},
	{Name: "Cherry Studio", URL: "cherrystudio://providers/api-keys?v=1&data={cherryConfig}"},
	{Name: "AionUI", URL: "aionui://provider/add?v=1&data={aionuiConfig}"},
	{Name: "流畅阅读", URL: "fluentread"},
	{Name: "CC Switch", URL: "ccswitch"},
	{Name: "DeepChat", URL: "deepchat://provider/install?v=1&data={deepchatConfig}"},
	{Name: "Lobe Chat 官方示例", URL: "https://chat-preview.lobehub.com/?settings={\"keyVaults\":{\"openai\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\"}}}"},
	{Name: "AI as Workspace", URL: "https://aiaw.app/set-provider?provider={\"type\":\"openai\",\"settings\":{\"apiKey\":\"{key}\",\"baseURL\":\"{address}/v1\",\"compatibility\":\"strict\"}}"},
	{Name: "AMA 问天", URL: "ama://set-api-key?server={address}&key={key}"},
	{Name: "OpenCat", URL: "opencat://team/join?domain={address}&token={key}"},
}

var chatsMutex sync.RWMutex

func parseChatsJsonString(jsonString string) ([]chatConfig, error) {
	var rawChats []map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(jsonString, &rawChats); err != nil {
		return nil, err
	}

	parsed := make([]chatConfig, 0, len(rawChats))
	for index, rawChat := range rawChats {
		if len(rawChat) != 1 {
			return nil, fmt.Errorf("chat preset %d must contain exactly one name and value", index+1)
		}

		for rawName, rawValue := range rawChat {
			name := strings.TrimSpace(rawName)
			if name == "" {
				return nil, fmt.Errorf("chat preset %d name is required", index+1)
			}

			config := chatConfig{Name: name}
			switch common.GetJsonType(rawValue) {
			case "string":
				if err := common.Unmarshal(rawValue, &config.URL); err != nil {
					return nil, fmt.Errorf("chat preset %q URL is invalid: %w", name, err)
				}
			case "object":
				var restricted restrictedChatConfig
				if err := common.Unmarshal(rawValue, &restricted); err != nil {
					return nil, fmt.Errorf("chat preset %q configuration is invalid: %w", name, err)
				}
				config.URL = restricted.URL
				if len(restricted.Groups) > maxChatPresetGroups {
					return nil, fmt.Errorf("chat preset %q may contain at most %d groups", name, maxChatPresetGroups)
				}
				seenGroups := make(map[string]struct{}, len(restricted.Groups))
				for _, rawGroup := range restricted.Groups {
					group := strings.TrimSpace(rawGroup)
					if group == "" || len(group) > 64 {
						return nil, fmt.Errorf("chat preset %q contains an invalid group", name)
					}
					if _, exists := seenGroups[group]; exists {
						continue
					}
					seenGroups[group] = struct{}{}
					config.Groups = append(config.Groups, group)
				}
			default:
				return nil, fmt.Errorf("chat preset %q value must be a URL string or an object", name)
			}

			config.URL = strings.TrimSpace(config.URL)
			if config.URL == "" {
				return nil, fmt.Errorf("chat preset %q URL is required", name)
			}
			parsed = append(parsed, config)
		}
	}
	return parsed, nil
}

func UpdateChatsByJsonString(jsonString string) error {
	parsed, err := parseChatsJsonString(jsonString)
	if err != nil {
		return err
	}
	chatsMutex.Lock()
	chats = parsed
	chatsMutex.Unlock()
	return nil
}

func ValidateChatsJsonString(jsonString string) error {
	_, err := parseChatsJsonString(jsonString)
	return err
}

func Chats2JsonString() string {
	chatsMutex.RLock()
	serialized := make([]map[string]any, 0, len(chats))
	for _, chat := range chats {
		if len(chat.Groups) == 0 {
			serialized = append(serialized, map[string]any{chat.Name: chat.URL})
			continue
		}
		serialized = append(serialized, map[string]any{
			chat.Name: restrictedChatConfig{
				URL:    chat.URL,
				Groups: append([]string(nil), chat.Groups...),
			},
		})
	}
	chatsMutex.RUnlock()

	jsonBytes, err := common.Marshal(serialized)
	if err != nil {
		common.SysLog("error marshalling chats: " + err.Error())
		return "[]"
	}
	return string(jsonBytes)
}

// GetChats returns only unrestricted presets for the anonymous status payload.
// Group-restricted presets are served through GetChatPresetsForGroups instead.
func GetChats() []map[string]string {
	chatsMutex.RLock()
	defer chatsMutex.RUnlock()

	result := make([]map[string]string, 0, len(chats))
	for _, chat := range chats {
		if len(chat.Groups) == 0 {
			result = append(result, map[string]string{chat.Name: chat.URL})
		}
	}
	return result
}

func GetChatPresetsForGroups(userGroups []string) []ChatPreset {
	groupSet := make(map[string]struct{}, len(userGroups))
	for _, rawGroup := range userGroups {
		if group := strings.TrimSpace(rawGroup); group != "" {
			groupSet[group] = struct{}{}
		}
	}

	chatsMutex.RLock()
	defer chatsMutex.RUnlock()

	result := make([]ChatPreset, 0, len(chats))
	for index, chat := range chats {
		allowed := len(chat.Groups) == 0
		if !allowed {
			for _, group := range chat.Groups {
				if _, exists := groupSet[group]; exists {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			continue
		}
		result = append(result, ChatPreset{
			ID:   strconv.Itoa(index),
			Name: chat.Name,
			URL:  chat.URL,
		})
	}
	return result
}
