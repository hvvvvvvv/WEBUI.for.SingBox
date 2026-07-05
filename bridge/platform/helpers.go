package platform

import (
	"net/http"
	"net/url"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func proxyFor(rawProxy string) func(*http.Request) (*url.URL, error) {
	proxy := http.ProxyFromEnvironment
	if rawProxy == "" {
		return proxy
	}
	proxyURL, err := url.Parse(rawProxy)
	if err != nil {
		return proxy
	}
	return http.ProxyURL(proxyURL)
}

func requestTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func decodeCommandOutput(data []byte) string {
	decoded, _ := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	return string(decoded)
}

func ConvertByte2String(data []byte) string {
	return decodeCommandOutput(data)
}

func GetProxy(rawProxy string) func(*http.Request) (*url.URL, error) {
	return proxyFor(rawProxy)
}

func GetTimeout(seconds int) time.Duration {
	return requestTimeout(seconds)
}
