package common

import "github.com/QuantumNous/new-api/relaykit/relayconvert"

func (info *RelayInfo) CloseConversionSession() {
	if info == nil {
		return
	}
	if info.Conversion != nil {
		info.Conversion.Close()
	}
	info.Conversion = nil
	info.ClaudeConvertInfo = nil
}

func (info *RelayInfo) ConversionSession() *relayconvert.ConversionSession {
	if info == nil {
		return relayconvert.NewConversionSession(info)
	}
	if info.Conversion == nil {
		info.Conversion = relayconvert.NewConversionSession(info)
		info.ClaudeConvertInfo = info.Conversion.ClaudeState()
	}
	return info.Conversion
}
