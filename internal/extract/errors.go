// Upstream error classification adapted from Migurdex (GPL-3.0),
// Migurdex.Shared/Diagnostics/UpstreamErrorClassifier.cs
package extract

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"aniclitr/internal/netx"
)

// Kind categorises why an upstream embed or stream failed.
type Kind int

const (
	KindUnknown Kind = iota
	KindNotFound
	KindDeleted
	KindPrivate
	KindQuota
	KindRateLimited
	KindEmbedBlocked
	KindCopyright
	KindChanged // the site's structure changed: the extractor needs updating
	KindBlocked // bot wall (Cloudflare etc.): never bypassed by policy
	KindProcessing
)

func (k Kind) String() string {
	switch k {
	case KindNotFound:
		return "not_found"
	case KindDeleted:
		return "deleted"
	case KindPrivate:
		return "private"
	case KindQuota:
		return "quota"
	case KindRateLimited:
		return "rate_limited"
	case KindEmbedBlocked:
		return "embed_blocked"
	case KindCopyright:
		return "copyright"
	case KindChanged:
		return "changed"
	case KindBlocked:
		return "blocked"
	case KindProcessing:
		return "processing"
	default:
		return "unknown"
	}
}

// Permanent reports whether this link will never become playable later in the session.
func (k Kind) Permanent() bool {
	switch k {
	case KindNotFound, KindDeleted, KindPrivate, KindCopyright, KindEmbedBlocked:
		return true
	}
	return false
}

// Message returns the concise Turkish explanation for the UI toast and picker.
func (k Kind) Message() string {
	switch k {
	case KindNotFound:
		return "kaynak bulunamadı (404)"
	case KindDeleted:
		return "bağlantı kaldırılmış"
	case KindCopyright:
		return "telif nedeniyle kaldırılmış"
	case KindPrivate:
		return "video gizli"
	case KindQuota:
		return "sunucu kotası dolmuş, sonra tekrar deneyin"
	case KindRateLimited:
		return "çok fazla istek; biraz bekleyin"
	case KindEmbedBlocked:
		return "gömülü oynatma engellenmiş"
	case KindBlocked:
		return "site otomatik erişimi engelliyor"
	case KindChanged:
		return "sitenin yapısı değişmiş"
	case KindProcessing:
		return "video henüz hazırlanıyor"
	default:
		return ""
	}
}

// Failure records one failed extractor attempt.
type Failure struct {
	Extractor string
	Kind      Kind
	Detail    string
	Err       error
}

func (f Failure) String() string {
	if msg := f.Kind.Message(); msg != "" {
		return fmt.Sprintf("%s: %s", f.Extractor, msg)
	}
	if f.Detail != "" {
		return fmt.Sprintf("%s: %s", f.Extractor, f.Detail)
	}
	if f.Err != nil {
		return fmt.Sprintf("%s: %v", f.Extractor, f.Err)
	}
	return f.Extractor + ": hata"
}

// Error is a typed extraction error returned by extractors and Resolve.
type Error struct {
	Extractor string
	Kind      Kind
	Detail    string
	Err       error
	Failures  []Failure
}

func (e *Error) Error() string {
	if msg := e.Kind.Message(); msg != "" {
		if e.Extractor != "" {
			return e.Extractor + ": " + msg
		}
		return msg
	}
	if e.Detail != "" {
		if e.Extractor != "" && !strings.HasPrefix(strings.ToLower(e.Detail), strings.ToLower(e.Extractor)+":") {
			return e.Extractor + ": " + e.Detail
		}
		return e.Detail
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "çözümleme hatası"
}

func (e *Error) Unwrap() error { return e.Err }

// NewError creates a typed extractor error.
func NewError(kind Kind, detail string) *Error {
	return &Error{Kind: kind, Detail: Summarize(detail, 200)}
}

// WrapError wraps err with a classified Kind and detail.
func WrapError(kind Kind, detail string, err error) *Error {
	if kind == KindUnknown && err != nil {
		kind = KindOf(err)
	}
	if detail == "" && err != nil {
		detail = err.Error()
	}
	return &Error{Kind: kind, Detail: Summarize(detail, 200), Err: err}
}

// KindOf extracts the upstream error Kind from any error chain.
func KindOf(err error) Kind {
	if err == nil {
		return KindUnknown
	}
	var ee *Error
	if errors.As(err, &ee) && ee.Kind != KindUnknown {
		return ee.Kind
	}
	var se *netx.StatusError
	if errors.As(err, &se) {
		return Classify(se.Code, se.Body)
	}
	return Classify(0, []byte(err.Error()))
}

// FailuresOf returns the recorded extractor failures from a Resolve error, if any.
func FailuresOf(err error) []Failure {
	var ee *Error
	if errors.As(err, &ee) {
		return ee.Failures
	}
	return nil
}

const maxScanBytes = 65536

var (
	quotaMarkers = []string{
		"quota exceeded", "quota_exceeded", "quotaexceeded", "exceeds the quota",
		"download quota", "playback quota", "kota",
	}
	copyrightMarkers = []string{
		"copyright claim", "copyrightsrestricted", "авторских прав",
		"at the request of the copyright holder", "telif hakkı",
	}
	privateMarkers = []string{
		`"private":`, "private content", "this video is private", "video is private",
		"özel içerik", "özel olarak", "login required", "sign in to watch", "giriş yap",
	}
	processingMarkers = []string{
		"publishing in progress", "publication of this video", "dm006",
		"video is being processed", "processing",
	}
	deletedMarkers = []string{
		"deleted", "has been removed", "no longer available", "video unavailable",
		"file not found", "not found", "does not exist", "doesn't exist",
		"silindi", "silinmiş", "kaldırıldı", "kaldırılmış", "bulunamadı", "mevcut değil",
		"удалено", "удален", "не найдено",
		"has not been found", "usernotfound",
	}
	embedMarkers = []string{
		"blocklist", "embedding disabled", "embed disabled", "not embeddable",
		`"embeddable":false`,
	}
	cloudflareMarkers = []string{
		"just a moment", "checking your browser", "__cf_bm", "cf-chl",
		"attention required", "cloudflare",
	}
	rateMarkers = []string{
		"too many requests", "rate limit", "rate-limit", "ratelimit", "slow down",
	}
)

// Classify inspects the HTTP status code and the first 64 KB of body.
// Order matters: quota is checked before deleted because quota pages often
// also say "not available".
func Classify(status int, body []byte) Kind {
	if len(body) > maxScanBytes {
		body = body[:maxScanBytes]
	}
	text := strings.ToLower(string(body))
	if text != "" {
		switch {
		case containsAny(text, quotaMarkers):
			return KindQuota
		case containsAny(text, copyrightMarkers):
			return KindCopyright
		case containsAny(text, privateMarkers):
			return KindPrivate
		case containsAny(text, processingMarkers):
			return KindProcessing
		case containsAny(text, deletedMarkers):
			return KindDeleted
		case containsAny(text, embedMarkers):
			return KindEmbedBlocked
		case containsAny(text, cloudflareMarkers):
			return KindBlocked
		case containsAny(text, rateMarkers):
			return KindRateLimited
		}
	}
	switch status {
	case 429:
		return KindRateLimited
	case 404:
		return KindNotFound
	case 410:
		return KindDeleted
	default:
		return KindUnknown
	}
}

func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// Summarize collapses whitespace to a single line and caps its length in runes.
func Summarize(text string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 200
	}
	single := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(single) <= maxRunes {
		return single
	}
	r := []rune(single)
	return string(r[:maxRunes]) + "…"
}

// DedupeFailures removes duplicate (Kind, Detail) entries while preserving order.
func DedupeFailures(fails []Failure) []Failure {
	if len(fails) <= 1 {
		return fails
	}
	type key struct {
		k Kind
		d string
	}
	seen := make(map[key]bool, len(fails))
	out := make([]Failure, 0, len(fails))
	for _, f := range fails {
		k := key{k: f.Kind, d: f.Detail}
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}
