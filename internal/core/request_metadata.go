package core

import (
	"context"
	"strings"

	"WantasticCore/internal/auth"
)

func requestUserAgent(ctx context.Context, explicit string) string {
	if ua := strings.TrimSpace(explicit); ua != "" {
		return ua
	}

	if cc := auth.CallContextFrom(ctx); cc != nil {
		if ua := strings.TrimSpace(cc.OriginUserAgent); ua != "" {
			return ua
		}
	}

	return ""
}

func requestClientIP(ctx context.Context) string {
	clientIP := "unknown"

	if cc := auth.CallContextFrom(ctx); cc != nil {
		if ip := strings.TrimSpace(cc.OriginIP); ip != "" {
			clientIP = ip
		}
	}

	return clientIP
}
