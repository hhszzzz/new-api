package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func ConvertResponse(c *gin.Context, info *relaycommon.RelayInfo, target types.RelayFormat, response any) (*relayconvert.ResponseResult, error) {
	result, err := info.ConversionSession().Response(c, target, response)
	if result != nil {
		info.RecordConversionDiagnostics(c, result.Diagnostics)
	}
	return result, err
}

func ConvertStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, target types.RelayFormat, response any) (*relayconvert.ResponseResult, error) {
	result, err := info.ConversionSession().StreamResponse(c, target, response)
	if result != nil {
		info.RecordConversionDiagnostics(c, result.Diagnostics)
	}
	return result, err
}

func ConvertStreamResponseChunk(c *gin.Context, info *relaycommon.RelayInfo, state *relayconvert.ResponseStreamState, response any) ([]relayconvert.ResponseResult, error) {
	results, err := info.ConversionSession().Stream(c, state, response)
	if state != nil {
		info.RecordConversionDiagnostics(c, state.Diagnostics())
	}
	return results, err
}

func FinalizeStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, state *relayconvert.ResponseStreamState) ([]relayconvert.ResponseResult, error) {
	results, err := info.ConversionSession().Finish(c, state)
	if state != nil {
		info.RecordConversionDiagnostics(c, state.Diagnostics())
	}
	return results, err
}
