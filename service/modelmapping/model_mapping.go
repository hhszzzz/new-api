package modelmapping

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type Result struct {
	Model  string
	Mapped bool
}

func Resolve(mappingJSON string, modelName string) (Result, error) {
	modelName = strings.TrimSpace(modelName)
	result := Result{Model: modelName}
	mappingJSON = strings.TrimSpace(mappingJSON)
	if mappingJSON == "" || mappingJSON == "{}" {
		return result, nil
	}

	modelMap := make(map[string]string)
	if err := common.Unmarshal([]byte(mappingJSON), &modelMap); err != nil {
		return Result{}, fmt.Errorf("unmarshal_model_mapping_failed: %w", err)
	}

	currentModel := modelName
	visitedModels := map[string]bool{currentModel: true}
	for {
		mappedModel, exists := modelMap[currentModel]
		mappedModel = strings.TrimSpace(mappedModel)
		if !exists || mappedModel == "" {
			break
		}
		if visitedModels[mappedModel] {
			if mappedModel == currentModel {
				if currentModel != modelName {
					result.Mapped = true
				}
				break
			}
			return Result{}, errors.New("model_mapping_contains_cycle")
		}
		visitedModels[mappedModel] = true
		currentModel = mappedModel
		result.Mapped = true
	}
	result.Model = currentModel
	return result, nil
}
