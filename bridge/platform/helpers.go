package platform

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func requestHeaders(headers map[string]string) http.Header {
	result := make(http.Header, len(headers))
	for key, value := range headers {
		result.Set(key, value)
	}
	return result
}

func decodeCommandOutput(data []byte) string {
	decoded, _ := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	return string(decoded)
}

func parseByteRange(value string, size int64) (start int64, end int64, err error) {
	if value == "" {
		return 0, size - 1, nil
	}
	value = strings.TrimPrefix(strings.TrimSpace(value), "bytes=")
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid range format")
	}
	startText := strings.TrimSpace(parts[0])
	endText := strings.TrimSpace(parts[1])
	if startText == "" && endText != "" {
		length, parseErr := strconv.ParseInt(endText, 10, 64)
		if parseErr != nil || length < 0 {
			return 0, 0, errors.New("invalid range value")
		}
		return max(0, size-length), size - 1, nil
	}
	if startText != "" {
		start, err = strconv.ParseInt(startText, 10, 64)
		if err != nil || start < 0 {
			return 0, 0, errors.New("invalid range value")
		}
	}
	if endText == "" {
		return start, size - 1, nil
	}
	end, err = strconv.ParseInt(endText, 10, 64)
	if err != nil || end < 0 {
		return 0, 0, errors.New("invalid range value")
	}
	if start > end {
		return 0, 0, errors.New("invalid range: start > end")
	}
	return start, min(end, size-1), nil
}

func ParseRange(value string, size int64) (int64, int64, error) {
	return parseByteRange(value, size)
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

func GetHeader(headers map[string]string) http.Header {
	return requestHeaders(headers)
}
