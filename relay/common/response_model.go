package common

import "strings"

// ResponseModel records upstream declarations before response conversion. It is
// diagnostic only: it must never change routing, pricing, or downstream output.
// Only the three names are stored; whether they disagree is computed on demand
// so every consumer applies the current comparison rule to old rows as well.
type ResponseModel struct {
	RequestedModel string `json:"requested_model"`
	UpstreamModel  string `json:"upstream_model"`
	ReturnedModel  string `json:"returned_model"`
}

// splitProviderPath separates a provider path prefix such as "vendor/" from
// the base model name. A name without a slash has an empty path. The path is
// significant: "vendor/mapped" and "other/mapped" are treated as different
// upstream models even though they share a base name.
func splitProviderPath(model string) (path, base string) {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		return model[:i+1], model[i+1:]
	}
	return "", model
}

// matches reports whether an upstream declaration is compatible with the
// requested or upstream model. The provider path must be identical; the base
// name must be equal ignoring case or extend it as a dated or variant name
// ("requested-2026-09-01"). A returned name behind a different provider path
// is a different model even when the base name matches, because gateways may
// route the same base name to quantized or otherwise distinct deployments.
func (r *ResponseModel) matches(model string) bool {
	returnedPath, returnedBase := splitProviderPath(strings.ToLower(model))
	for _, expected := range []string{r.RequestedModel, r.UpstreamModel} {
		expectedPath, expectedBase := splitProviderPath(strings.ToLower(expected))
		if expectedBase == "" || returnedPath != expectedPath {
			continue
		}
		if returnedBase == expectedBase || strings.HasPrefix(returnedBase, expectedBase) {
			return true
		}
	}
	return false
}

// Mismatch reports whether the retained upstream declaration disagrees with
// both the requested and upstream models.
func (r *ResponseModel) Mismatch() bool {
	return r != nil && r.ReturnedModel != "" && !r.matches(r.ReturnedModel)
}

// ObserveResponseModel retains the first differing model for inspection, with
// mismatches taking priority over provider-path, prefix, or case-only
// differences. A later matching or empty event cannot erase it. Only observe
// upstream declarations, never models synthesized by a response converter.
func (info *RelayInfo) ObserveResponseModel(model string) {
	if info == nil || strings.TrimSpace(model) == "" {
		return
	}
	if info.ResponseModel == nil {
		info.ResponseModel = &ResponseModel{
			RequestedModel: info.OriginModelName,
			UpstreamModel:  info.GetUpstreamModelName(),
		}
	}
	observation := info.ResponseModel
	if observation.Mismatch() {
		return
	}
	if observation.matches(model) && observation.ReturnedModel != "" &&
		observation.ReturnedModel != observation.RequestedModel && observation.ReturnedModel != observation.UpstreamModel {
		return
	}
	observation.ReturnedModel = model
}
