// Package contactlink 统一主站与分销站客服联系方式链接的校验规则。
package contactlink

import (
	"net/mail"
	"net/url"
	"strings"
)

// IsTelegram 判断是否为 Telegram 联系链接。
func IsTelegram(value string) bool {
	return strings.HasPrefix(value, "https://telegram.me/") ||
		strings.HasPrefix(value, "https://t.me/") ||
		strings.HasPrefix(value, "tg://")
}

// IsWhatsApp 判断是否为 WhatsApp 联系链接。
func IsWhatsApp(value string) bool {
	return strings.HasPrefix(value, "https://wa.me/") ||
		strings.HasPrefix(value, "https://api.whatsapp.com/")
}

// IsEmail accepts a single email address suitable for a mailto link.
func IsEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && !strings.ContainsAny(value, "\r\n?#")
}

// IsX accepts HTTPS links on X and its former Twitter domain.
func IsX(value string) bool {
	link, err := url.Parse(value)
	if err != nil || link.Scheme != "https" || link.User != nil || strings.Trim(link.Path, "/") == "" {
		return false
	}
	switch strings.ToLower(link.Host) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		return true
	default:
		return false
	}
}
