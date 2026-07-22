package controller

import (
	"sort"

	"github.com/gin-gonic/gin"
)

func getVisibleModelNames(c *gin.Context) []string {
	pricing, _, _, _ := getVisiblePricing(c)
	seen := make(map[string]struct{}, len(pricing))
	modelNames := make([]string, 0, len(pricing))
	for _, item := range pricing {
		if item.ModelName == "" {
			continue
		}
		if _, ok := seen[item.ModelName]; ok {
			continue
		}
		seen[item.ModelName] = struct{}{}
		modelNames = append(modelNames, item.ModelName)
	}
	sort.Strings(modelNames)
	return modelNames
}
