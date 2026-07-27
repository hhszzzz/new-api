/**
此文件为旧版支付设置文件，如需增加新的参数、变量等，请在 payment_setting.go 中添加
This file is the old version of the payment settings file. If you need to add new parameters, variables, etc., please add them in payment_setting.go
*/

package operation_setting

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

var PayAddress = ""
var CustomCallbackAddress = ""
var EpayId = ""
var EpayKey = ""
var Price = 7.3
var MinTopUp = 1
var USDExchangeRate = 7.3

var priceBits atomic.Uint64
var minTopUpValue atomic.Int64
var usdExchangeRateBits atomic.Uint64

var payMethods = []map[string]string{
	{
		"name": "支付宝",
		"icon": "SiAlipay",
		"type": "alipay",
	},
	{
		"name": "微信",
		"icon": "SiWechat",
		"type": "wxpay",
	},
	{
		"name":      "自定义1",
		"icon":      "LuCreditCard",
		"type":      "custom1",
		"min_topup": "50",
	},
}
var payMethodsMutex sync.RWMutex

func init() {
	SetPrice(Price)
	SetMinTopUp(MinTopUp)
	SetUSDExchangeRate(USDExchangeRate)
}

func SetPrice(value float64) {
	Price = value
	priceBits.Store(math.Float64bits(value))
}

func GetPrice() float64 {
	return math.Float64frombits(priceBits.Load())
}

func SetMinTopUp(value int) {
	MinTopUp = value
	minTopUpValue.Store(int64(value))
}

func GetMinTopUp() int {
	return int(minTopUpValue.Load())
}

func SetUSDExchangeRate(value float64) {
	USDExchangeRate = value
	usdExchangeRateBits.Store(math.Float64bits(value))
}

func GetUSDExchangeRate() float64 {
	return math.Float64frombits(usdExchangeRateBits.Load())
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	var parsed []map[string]string
	if err := common.UnmarshalJsonStr(jsonString, &parsed); err != nil {
		return err
	}
	payMethodsMutex.Lock()
	payMethods = parsed
	payMethodsMutex.Unlock()
	return nil
}

func PayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(GetPayMethods())
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	payMethodsMutex.RLock()
	defer payMethodsMutex.RUnlock()
	for _, payMethod := range payMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}

func GetPayMethods() []map[string]string {
	payMethodsMutex.RLock()
	defer payMethodsMutex.RUnlock()

	result := make([]map[string]string, len(payMethods))
	for index, method := range payMethods {
		result[index] = make(map[string]string, len(method))
		for key, value := range method {
			result[index][key] = value
		}
	}
	return result
}
