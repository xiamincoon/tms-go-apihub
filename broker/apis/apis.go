package apis

import (
	"github.com/jasony62/tms-go-apihub/core"
	"github.com/jasony62/tms-go-apihub/hub"
	klog "k8s.io/klog/v2"
)

func init() {
	klog.Infof("APIs register apis\n")
	core.RegisterApis(map[string]hub.ApiHandler{"httpApi": runHttpApi,
		"httpResponse":         httpResponse,
		"checkStringsEqual":    checkStringsEqual,
		"checkStringsNotEqual": checkStringsNotEqual,
		"createJson":           createJson,
		"createHtml":           createHtml,
	})
}