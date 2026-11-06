package extract

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

type voeExtractor struct{}

func init() { Register(voeExtractor{}) }

func (voeExtractor) Name() string { return "Voe" }

func (voeExtractor) CanHandle(u *url.URL) bool {
	if u == nil {
		return false
	}
	h := strings.ToLower(u.Host)
	return strings.Contains(h, "voe.sx") || strings.HasPrefix(h, "voe.")
}

func (voeExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return voeResolve(ctx, u, hdr)
}

var (
	reVoeRedirect   = regexp.MustCompile(`window\.location\.href\s*=\s*['"](https?://[^'"]+)['"]`)
	reVoeScriptJSON = regexp.MustCompile(`(?is)<script[^>]+type=['"]application/json['"][^>]*>\s*(\[[^<]+\])\s*</script>`)
	reVoePatterns   = regexp.MustCompile(`@\$|\^\^|~@|%\?|\*~|!!|#&`)
	reVoeHlsPlain   = regexp.MustCompile(`['"]hls['"]\s*:\s*['"]([^'"]+)['"]`)
)

// Voe resolves voe.sx embed pages using its 6-step payload deobfuscation pipeline.
func Voe(ctx context.Context, embed string) (*source.Stream, error) {
	u, err := url.Parse(NormalizeURL(embed))
	if err != nil {
		return nil, err
	}
	return voeResolve(ctx, u, nil)
}

func voeResolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	reqHdr := map[string]string{"Referer": u.String()}
	for k, v := range hdr {
		reqHdr[k] = v
	}
	body, err := netx.Get(ctx, u.String(), reqHdr)
	if err != nil {
		kind := KindOf(err)
		return nil, &Error{Extractor: "Voe", Kind: kind, Detail: err.Error(), Err: err}
	}

	if m := reVoeRedirect.FindSubmatch(body); m != nil {
		redirURL := strings.TrimSpace(string(m[1]))
		if redirURL != "" {
			body, err = netx.Get(ctx, redirURL, map[string]string{"Referer": u.String()})
			if err != nil {
				kind := KindOf(err)
				return nil, &Error{Extractor: "Voe", Kind: kind, Detail: err.Error(), Err: err}
			}
		}
	}

	st := &source.Stream{
		Referer:   "https://voe.sx/",
		UserAgent: netx.UserAgent,
	}

	if m := reVoeScriptJSON.FindSubmatch(body); m != nil {
		var arr []string
		if err := json.Unmarshal(m[1], &arr); err == nil && len(arr) > 0 && arr[0] != "" {
			if dec, err := voeDecrypt(arr[0]); err == nil {
				var payload struct {
					Source          string `json:"source"`
					DirectAccessURL string `json:"direct_access_url"`
				}
				if json.Unmarshal(dec, &payload) == nil {
					if s := strings.TrimSpace(payload.Source); s != "" {
						st.Qualities = append(st.Qualities, source.Quality{Label: "auto", URL: s})
					}
					if d := strings.TrimSpace(payload.DirectAccessURL); d != "" && len(st.Qualities) == 0 {
						st.Qualities = append(st.Qualities, source.Quality{Label: "mp4", URL: d})
					}
				}
			}
		}
	}

	if len(st.Qualities) == 0 {
		if m := reVoeHlsPlain.FindSubmatch(body); m != nil {
			raw := strings.TrimSpace(string(m[1]))
			if dec, err := voeBase64Decode(raw); err == nil && strings.HasPrefix(string(dec), "http") {
				raw = string(dec)
			}
			if raw != "" {
				st.Qualities = append(st.Qualities, source.Quality{Label: "auto", URL: raw})
			}
		}
	}

	if len(st.Qualities) == 0 {
		kind := Classify(200, body)
		if kind == KindUnknown {
			kind = KindChanged
		}
		return nil, &Error{Extractor: "Voe", Kind: kind, Detail: "voe: oynatılabilir akış bulunamadı"}
	}
	return st, nil
}

func voeDecrypt(encoded string) ([]byte, error) {
	step1 := voeRot13(encoded)
	step2 := reVoePatterns.ReplaceAllString(step1, "_")
	step3 := strings.ReplaceAll(step2, "_", "")
	step4, err := voeBase64Decode(step3)
	if err != nil {
		return nil, err
	}
	step5 := voeCharShift(string(step4), 3)
	step6 := voeReverse(step5)
	return voeBase64Decode(step6)
}

func voeRot13(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b.WriteByte('A' + (c-'A'+13)%26)
		case c >= 'a' && c <= 'z':
			b.WriteByte('a' + (c-'a'+13)%26)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func voeBase64Decode(s string) ([]byte, error) {
	clean := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(s))
	switch len(clean) % 4 {
	case 2:
		clean += "=="
	case 3:
		clean += "="
	}
	return base64.StdEncoding.DecodeString(clean)
}

func voeCharShift(s string, shift int) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = byte(int(s[i]) - shift)
	}
	return string(out)
}

func voeReverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
