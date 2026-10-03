package parser

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

func applySnell(node Node, out map[string]any) error {
	version, ok := nodeInt(node, "version")
	if !ok {
		return errors.New("invalid or missing Snell version")
	}
	if version != 4 && version != 5 && version != 6 {
		return errors.New("unsupported Snell version")
	}
	if quicValue := nodeValue(node, "quic", "quic-mode", "use-quic"); quicValue != nil {
		quic, valid := boolValue(quicValue)
		if !valid || quic {
			return errors.New("unsupported Snell QUIC mode")
		}
	}
	if strings.EqualFold(nodeString(node, "network"), "quic") ||
		strings.EqualFold(nodeString(node, "transport"), "quic") ||
		strings.EqualFold(mapString(nodeMap(node, "transport"), "type"), "quic") {
		return errors.New("unsupported Snell QUIC mode")
	}
	if tlsRequested(node) {
		return errors.New("unsupported Snell TLS transport")
	}
	if plugin := nodeString(node, "plugin"); plugin != "" && !strings.EqualFold(plugin, "none") {
		return errors.New("unsupported Snell obfuscation chain")
	}
	for key, value := range node {
		canonical := canonicalKey(key)
		if strings.HasPrefix(canonical, "shadowtls") || strings.HasPrefix(canonical, "restls") || strings.HasPrefix(canonical, "jls") {
			if disabled, valid := boolValue(value); valid && !disabled {
				continue
			}
			if value != nil && stringValue(value) != "" {
				return errors.New("unsupported Snell obfuscation chain")
			}
		}
	}
	if version == 5 {
		// Non-QUIC Snell v5 uses the v4 client wire protocol.
		version = 4
	}
	psk := protocolCredentialString(nodeValue(node, "psk", "password", "pass"))
	if psk == "" {
		return errors.New("missing required Snell credentials")
	}
	if version == 6 && (len(psk) < 12 || len(psk) > 255) {
		return errors.New("invalid Snell v6 pre-shared key length")
	}
	userKey := protocolCredentialString(nodeValue(node, "userkey", "user-key"))
	if len(userKey) > 255 {
		return errors.New("invalid Snell user key length")
	}
	out["version"], out["psk"] = version, psk
	setString(out, "userkey", userKey)
	copyOptionalBool(node, out, "reuse", "reuse")

	obfsOptions := nodeMap(node, "obfs-opts", "obfs-options", "obfs")
	obfsMode := firstNonEmpty(mapString(obfsOptions, "mode", "type"), nodeString(node, "obfs-mode"))
	if obfsMode == "" && nodeMap(node, "obfs") == nil {
		obfsMode = nodeString(node, "obfs")
	}
	obfsMode = strings.ToLower(obfsMode)
	obfsHost := firstNonEmpty(mapString(obfsOptions, "host", "obfs-host"), nodeString(node, "obfs-host"))
	mode := strings.ToLower(nodeString(node, "mode"))
	if version == 4 {
		if nodeValue(node, "mode") != nil {
			return errors.New("Snell traffic shaping requires version 6")
		}
		if obfsMode != "" && obfsMode != "none" && obfsMode != "http" && obfsMode != "tls" {
			return errors.New("unsupported Snell obfuscation mode")
		}
		setString(out, "obfs_mode", obfsMode)
		setString(out, "obfs_host", obfsHost)
	} else {
		if nodeValue(node, "obfs", "obfs-opts", "obfs-options", "obfs-mode", "obfs-host") != nil {
			return errors.New("Snell obfuscation requires version 4")
		}
		if mode != "" && mode != "default" && mode != "unshaped" && mode != "unsafe-raw" {
			return errors.New("unsupported Snell traffic shaping mode")
		}
		setString(out, "mode", mode)
	}
	return nil
}

// Credentials are byte strings: whitespace can be part of authentication data
// and must also count toward the protocol's length limits.
func protocolCredentialString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return stringValue(value)
}

func applyHysteria2HopIntervals(node Node, out map[string]any) error {
	minValue := nodeValue(node, "hop-interval", "hop_interval")
	maxValue := nodeValue(node, "hop-interval-max", "hop_interval_max")
	minText := strings.TrimSpace(stringValue(minValue))
	maxText := strings.TrimSpace(stringValue(maxValue))
	if minText == "" && maxText == "" {
		return nil
	}
	if minText == "" {
		return errors.New("Hysteria2 maximum hop interval requires a minimum")
	}
	var rangeMaxText string
	if lower, upper, isRange := strings.Cut(minText, "-"); isRange {
		// Subscription interval ranges are expressed as seconds, e.g. 15-30.
		if _, ok := positiveSeconds(lower); !ok {
			return errors.New("invalid Hysteria2 hop interval range")
		}
		if _, ok := positiveSeconds(upper); !ok {
			return errors.New("invalid Hysteria2 hop interval range")
		}
		minText, rangeMaxText = strings.TrimSpace(lower), strings.TrimSpace(upper)
	}
	minText, minDuration, err := protocolDurationSeconds(minText)
	if err != nil {
		return errors.New("invalid Hysteria2 hop interval")
	}
	if minDuration < 5*time.Second {
		return errors.New("Hysteria2 hop interval must be at least 5 seconds")
	}
	var maxDuration time.Duration
	if rangeMaxText != "" {
		rangeText, rangeDuration, err := protocolDurationSeconds(rangeMaxText)
		if err != nil {
			return errors.New("invalid Hysteria2 hop interval range")
		}
		if maxText != "" {
			_, explicitDuration, err := protocolDurationSeconds(maxText)
			if err != nil || explicitDuration != rangeDuration {
				return errors.New("conflicting Hysteria2 maximum hop intervals")
			}
		}
		maxText, maxDuration = rangeText, rangeDuration
	} else if maxText != "" {
		maxText, maxDuration, err = protocolDurationSeconds(maxText)
		if err != nil {
			return errors.New("invalid Hysteria2 maximum hop interval")
		}
	}
	if maxText != "" && minDuration > maxDuration {
		return errors.New("Hysteria2 minimum hop interval exceeds maximum")
	}
	out["hop_interval"] = minText
	setString(out, "hop_interval_max", maxText)
	return nil
}

func positiveSeconds(text string) (float64, bool) {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return seconds, err == nil && seconds > 0 && !math.IsInf(seconds, 0) && !math.IsNaN(seconds)
}

func protocolDurationSeconds(text string) (string, time.Duration, error) {
	text = strings.TrimSpace(text)
	if _, ok := positiveSeconds(text); ok {
		text += "s"
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration <= 0 {
		return "", 0, errors.New("invalid proxy duration")
	}
	return text, duration, nil
}

func firstProtocolValue(options map[string]any, node Node, optionKeys, nodeKeys []string) any {
	if value := mapValue(options, optionKeys...); value != nil {
		return value
	}
	return nodeValue(node, nodeKeys...)
}

func applyGeckoPacketSizes(obfs map[string]any, minValue, maxValue any) error {
	minSize, maxSize := 512, 1200
	for _, packetSize := range []struct {
		key   string
		value any
		size  *int
	}{
		{"min_packet_size", minValue, &minSize},
		{"max_packet_size", maxValue, &maxSize},
	} {
		if packetSize.value == nil {
			continue
		}
		size, valid := intValue(packetSize.value)
		if !valid || size < 0 || size > 2048 {
			return errors.New("invalid Hysteria2 Gecko packet size")
		}
		if size > 0 {
			*packetSize.size = size
			obfs[packetSize.key] = size
		}
	}
	if minSize > maxSize {
		return errors.New("Hysteria2 Gecko minimum packet size exceeds maximum")
	}
	return nil
}
