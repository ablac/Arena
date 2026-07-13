package game

import (
	"strings"
	"unicode/utf8"
)

const websocketCloseReasonMaxBytes = 123

// boundedWebSocketCloseReason keeps a close reason within the 125-byte control
// frame limit after the two-byte status code, without cutting a UTF-8 rune.
func boundedWebSocketCloseReason(reason string) string {
	if !utf8.ValidString(reason) {
		reason = strings.ToValidUTF8(reason, "")
	}
	if len(reason) <= websocketCloseReasonMaxBytes {
		return reason
	}
	reason = reason[:websocketCloseReasonMaxBytes]
	for !utf8.ValidString(reason) {
		reason = reason[:len(reason)-1]
	}
	return reason
}
