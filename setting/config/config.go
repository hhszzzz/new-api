package config

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ConfigManager 统一管理所有配置
type ConfigManager struct {
	configs map[string]interface{}
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

// ConfigValidator validates a complete candidate before it becomes visible.
type ConfigValidator interface {
	ValidateConfig() error
}

// ConfigLoadValidator may apply a different semantic policy to persisted
// configuration. Parsing errors are always rejected before this hook runs.
type ConfigLoadValidator interface {
	ValidateLoadedConfig() error
}

// ConfigPublisher refreshes immutable runtime state after a config update.
type ConfigPublisher interface {
	PublishConfig()
}

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]interface{}),
	}
}

// Register 注册一个配置模块
func (cm *ConfigManager) Register(name string, config interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = config
}

// Get 获取指定配置模块
func (cm *ConfigManager) Get(name string) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.configs[name]
}

// LoadFromDB 从数据库加载配置
func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	type preparedConfig struct {
		config    interface{}
		candidate reflect.Value
	}
	prepared := make([]preparedConfig, 0, len(cm.configs))
	names := make([]string, 0, len(cm.configs))
	for name := range cm.configs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		registeredConfig := cm.configs[name]
		prefix := name + "."
		configMap := make(map[string]string)

		// 收集属于此配置的所有选项
		for key, value := range options {
			if strings.HasPrefix(key, prefix) {
				configKey := strings.TrimPrefix(key, prefix)
				configMap[configKey] = value
			}
		}

		if len(configMap) == 0 {
			continue
		}
		candidate, err := prepareConfigUpdate(registeredConfig, configMap, true)
		if err != nil {
			return fmt.Errorf("failed to update config %s: %w", name, err)
		}
		prepared = append(prepared, preparedConfig{config: registeredConfig, candidate: candidate})
	}

	for _, update := range prepared {
		applyConfigUpdate(update.config, update.candidate)
	}

	return nil
}

// ValidateUpdate validates a partial update without publishing it. The bool is
// false when the requested config module is not registered.
func (cm *ConfigManager) ValidateUpdate(name string, configMap map[string]string) (bool, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	registeredConfig, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	_, err := prepareConfigUpdate(registeredConfig, configMap, false)
	return true, err
}

// Update atomically validates and publishes a partial config update. The bool
// is false when the requested config module is not registered.
func (cm *ConfigManager) Update(name string, configMap map[string]string) (bool, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	registeredConfig, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	candidate, err := prepareConfigUpdate(registeredConfig, configMap, false)
	if err != nil {
		return true, err
	}
	applyConfigUpdate(registeredConfig, candidate)
	return true, nil
}

// UpdateFromDB parses and publishes one persisted config update. Configs may
// opt into a fail-closed normalization policy through ConfigLoadValidator.
func (cm *ConfigManager) UpdateFromDB(name string, configMap map[string]string) (bool, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	registeredConfig, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	candidate, err := prepareConfigUpdate(registeredConfig, configMap, true)
	if err != nil {
		return true, err
	}
	applyConfigUpdate(registeredConfig, candidate)
	return true, nil
}

// SaveToDB 将配置保存到数据库
func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			return err
		}

		for key, value := range configMap {
			dbKey := name + "." + key
			if err := updateFunc(dbKey, value); err != nil {
				return err
			}
		}
	}

	return nil
}

// 辅助函数：将配置对象转换为map
func configToMap(config interface{}) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		key, include := configFieldKey(fieldType)
		if !include {
			continue
		}

		// 处理不同类型的字段
		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, field.Type().Bits())
		case reflect.Ptr:
			// 处理指针类型：如果非 nil，序列化指向的值
			if !field.IsNil() {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			} else {
				// nil 指针序列化为 "null"
				strValue = "null"
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// 复杂类型使用JSON序列化
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			// 跳过不支持的类型
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

func configFieldKey(field reflect.StructField) (string, bool) {
	key := field.Tag.Get("json")
	if comma := strings.IndexByte(key, ','); comma >= 0 {
		key = key[:comma]
	}
	if key == "-" {
		return "", false
	}
	if key == "" {
		key = field.Name
	}
	return key, true
}

func prepareConfigUpdate(config interface{}, configMap map[string]string, fromDB bool) (reflect.Value, error) {
	target := reflect.ValueOf(config)
	if !target.IsValid() || target.Kind() != reflect.Ptr || target.IsNil() {
		return reflect.Value{}, fmt.Errorf("config must be a non-nil pointer")
	}
	if target.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("config must point to a struct")
	}

	candidate := reflect.New(target.Elem().Type())
	candidate.Elem().Set(target.Elem())
	val := candidate.Elem()
	typ := val.Type()
	matchedKeys := make(map[string]struct{}, len(configMap))
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		key, include := configFieldKey(fieldType)
		if !include {
			continue
		}

		// 检查map中是否有对应的值
		strValue, ok := configMap[key]
		if !ok {
			continue
		}
		matchedKeys[key] = struct{}{}

		if !field.CanSet() {
			return reflect.Value{}, fmt.Errorf("config field %s cannot be set", key)
		}
		if err := parseConfigField(field, strValue); err != nil {
			return reflect.Value{}, fmt.Errorf("invalid config field %s: %w", key, err)
		}
	}

	for key := range configMap {
		if _, ok := matchedKeys[key]; !ok {
			if fromDB {
				continue
			}
			return reflect.Value{}, fmt.Errorf("unknown config field %s", key)
		}
	}
	validator, hasValidator := candidate.Interface().(ConfigValidator)
	if fromDB {
		if loadValidator, ok := candidate.Interface().(ConfigLoadValidator); ok {
			if err := loadValidator.ValidateLoadedConfig(); err != nil {
				return reflect.Value{}, err
			}
		} else if hasValidator {
			if err := validator.ValidateConfig(); err != nil {
				return reflect.Value{}, err
			}
		}
	} else if hasValidator {
		if err := validator.ValidateConfig(); err != nil {
			return reflect.Value{}, err
		}
	}
	return candidate, nil
}

func parseConfigField(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			floatValue, floatErr := strconv.ParseFloat(value, 64)
			if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || math.Trunc(floatValue) != floatValue {
				return err
			}
			parsed, err = strconv.ParseInt(strconv.FormatFloat(floatValue, 'f', -1, 64), 10, field.Type().Bits())
			if err != nil {
				return err
			}
		}
		field.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, field.Type().Bits())
		if err != nil {
			floatValue, floatErr := strconv.ParseFloat(value, 64)
			if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 || math.Trunc(floatValue) != floatValue {
				return err
			}
			parsed, err = strconv.ParseUint(strconv.FormatFloat(floatValue, 'f', -1, 64), 10, field.Type().Bits())
			if err != nil {
				return err
			}
		}
		field.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil {
			return err
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("value must be finite")
		}
		field.SetFloat(parsed)
	case reflect.Ptr:
		if value == "null" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		fresh := reflect.New(field.Type().Elem())
		if err := common.UnmarshalJsonStr(value, fresh.Interface()); err != nil {
			return err
		}
		field.Set(fresh)
	case reflect.Map, reflect.Slice, reflect.Struct:
		fresh := reflect.New(field.Type())
		if err := common.UnmarshalJsonStr(value, fresh.Interface()); err != nil {
			return err
		}
		field.Set(fresh.Elem())
	default:
		return fmt.Errorf("unsupported type %s", field.Type())
	}
	return nil
}

func applyConfigUpdate(config interface{}, candidate reflect.Value) {
	reflect.ValueOf(config).Elem().Set(candidate.Elem())
	if publisher, ok := config.(ConfigPublisher); ok {
		publisher.PublishConfig()
	}
}

// 辅助函数：从map更新配置对象
func updateConfigFromMap(config interface{}, configMap map[string]string) error {
	candidate, err := prepareConfigUpdate(config, configMap, false)
	if err != nil {
		return err
	}
	applyConfigUpdate(config, candidate)
	return nil
}

// ConfigToMap 将配置对象转换为map（导出函数）
func ConfigToMap(config interface{}) (map[string]string, error) {
	return configToMap(config)
}

// UpdateConfigFromMap 从map更新配置对象（导出函数）
func UpdateConfigFromMap(config interface{}, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

// ExportAllConfigs 导出所有已注册的配置为扁平结构
func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		// 使用 "模块名.配置项" 的格式添加到结果中
		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
