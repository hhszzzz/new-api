package setting

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	maxCustomHeaderNavItems          = 20
	maxCustomHeaderNavIconNameLength = 80
)

var customHeaderNavIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var customHeaderNavIconPattern = regexp.MustCompile(`^(Ai|Bi|Bs|Cg|Ci|Di|Fa|Fc|Fi|Gi|Go|Gr|Hi|Im|Io|Lia|Lu|Md|Pi|Ri|Rx|Si|Sl|Tb|Tfi|Ti|Vsc|Wi)[A-Z0-9][A-Za-z0-9]*$`)

type customHeaderNavItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Icon    string `json:"icon,omitempty"`
	Enabled bool   `json:"enabled"`
}

type headerNavModulesOption struct {
	Custom []customHeaderNavItem `json:"custom"`
	Order  []string              `json:"order"`
}

func ValidateHeaderNavModules(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var config headerNavModulesOption
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return fmt.Errorf("header navigation must be a JSON object: %w", err)
	}
	if len(config.Custom) > maxCustomHeaderNavItems {
		return fmt.Errorf("custom header navigation cannot contain more than %d items", maxCustomHeaderNavItems)
	}

	customIDs := make(map[string]struct{}, len(config.Custom))
	for index, item := range config.Custom {
		if !customHeaderNavIDPattern.MatchString(item.ID) {
			return fmt.Errorf("custom header navigation item %d has an invalid id", index+1)
		}
		if _, duplicate := customIDs[item.ID]; duplicate {
			return fmt.Errorf("custom header navigation item %d has a duplicate id", index+1)
		}
		customIDs[item.ID] = struct{}{}

		title := strings.TrimSpace(item.Title)
		if title == "" || utf8.RuneCountInString(title) > 40 {
			return fmt.Errorf("custom header navigation item %d must have a title of 1 to 40 characters", index+1)
		}

		itemURL := strings.TrimSpace(item.URL)
		if len(itemURL) == 0 || len(itemURL) > 2048 {
			return fmt.Errorf("custom header navigation item %d has an invalid URL length", index+1)
		}
		parsedURL, err := url.Parse(itemURL)
		if err != nil || parsedURL.Host == "" ||
			(!strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https")) {
			return fmt.Errorf("custom header navigation item %d must use a valid HTTP or HTTPS URL", index+1)
		}
		if parsedURL.User != nil {
			return fmt.Errorf("custom header navigation item %d URL cannot contain credentials", index+1)
		}

		icon := strings.TrimSpace(item.Icon)
		if icon != "" && (len(icon) > maxCustomHeaderNavIconNameLength || !customHeaderNavIconPattern.MatchString(icon)) {
			return fmt.Errorf("custom header navigation item %d has an invalid React Icons icon name", index+1)
		}
	}

	allowedOrderKeys := map[string]struct{}{
		"home": {}, "console": {}, "pricing": {}, "modelStatus": {},
		"modelRadar": {}, "rankings": {}, "docs": {}, "about": {},
	}
	for id := range customIDs {
		allowedOrderKeys["custom:"+id] = struct{}{}
	}
	seenOrderKeys := make(map[string]struct{}, len(config.Order))
	for _, key := range config.Order {
		if _, allowed := allowedOrderKeys[key]; !allowed {
			return fmt.Errorf("header navigation order contains unknown item %q", key)
		}
		if _, duplicate := seenOrderKeys[key]; duplicate {
			return fmt.Errorf("header navigation order contains duplicate item %q", key)
		}
		seenOrderKeys[key] = struct{}{}
	}

	return nil
}
