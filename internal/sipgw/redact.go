package sipgw

import (
	"github.com/boa-z/vohive/pkg/logger"
)

func shouldLogSIPRaw() bool {
	return logger.ShouldLogSIPRaw()
}

func redactSIPRaw(raw string) string {
	return logger.RedactSIPRaw(raw)
}
