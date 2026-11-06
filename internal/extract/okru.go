package extract

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

type okruExtractor struct{}

func init() { Register(okruExtractor{}) }

func (okruExtractor) Name() string { return "OK.ru" }

func (okruExtractor) CanHandle(u *url.URL) bool {
	if u == nil {
		return false
	}
	h := strings.ToLower(u.Host)
	return strings.Contains(h, "ok.ru") || strings.Contains(h, "odnoklassniki.ru")
}

func (okruExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return okruResolve(ctx, u, hdr)
}

var (
	reOkRuOptions = regexp.MustCompile(`data-options="([^"]+)"`)
	reOkRuVideoID = regexp.MustCompile(`(?:videoembed|video)/(\d+)`)
)

// OkRu resolves ok.ru / odnoklassniki.ru embed pages directly without yt-dlp.
func OkRu(ctx context.Context, embed string) (*source.Stream, error) {
	u, err := url.Parse(NormalizeURL(embed))
	if err != nil {
		return nil, err
	}
	return okruResolve(ctx, u, nil)
}

func okruResolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	target := u.String()
	// Normalise /video/{id} to /videoembed/{id}
	if !strings.Contains(u.Path, "videoembed") {
		if m := reOkRuVideoID.FindStringSubmatch(u.Path); m != nil {
			origin := "https://ok.ru"
			if u.Scheme != "" && u.Host != "" {
				origin = u.Scheme + "://" + u.Host
			}
			target = origin + "/videoembed/" + m[1]
		}
	}

	reqHdr := map[string]string{"Referer": "https://ok.ru/"}
	for k, v := range hdr {
		reqHdr[k] = v
	}
	body, err := netx.Get(ctx, target, reqHdr)
	if err != nil {
		kind := KindOf(err)
		return nil, &Error{Extractor: "OK.ru", Kind: kind, Detail: err.Error(), Err: err}
	}

	m := reOkRuOptions.FindSubmatch(body)
	if m == nil {
		kind := Classify(200, body)
		if kind == KindUnknown {
			kind = KindChanged
		}
		return nil, &Error{Extractor: "OK.ru", Kind: kind, Detail: "ok.ru: data-options bulunamadı"}
	}

	decoded := html.UnescapeString(string(m[1]))
	var opts struct {
		Flashvars struct {
			Metadata json.RawMessage `json:"metadata"`
		} `json:"flashvars"`
	}
	if err := json.Unmarshal([]byte(decoded), &opts); err != nil || len(opts.Flashvars.Metadata) == 0 {
		return nil, &Error{Extractor: "OK.ru", Kind: KindChanged, Detail: "ok.ru: flashvars okunamadı", Err: err}
	}

	var metaRaw []byte
	var metaStr string
	if err := json.Unmarshal(opts.Flashvars.Metadata, &metaStr); err == nil {
		metaRaw = []byte(metaStr)
	} else {
		metaRaw = opts.Flashvars.Metadata
	}

	var meta struct {
		HlsManifestURL string `json:"hlsManifestUrl"`
		OndemandHls    string `json:"ondemandHls"`
		Videos         []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"videos"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, &Error{Extractor: "OK.ru", Kind: KindChanged, Detail: "ok.ru: metadata okunamadı", Err: err}
	}

	st := &source.Stream{
		Referer:   "https://ok.ru/",
		UserAgent: netx.UserAgent,
	}
	seen := map[string]bool{}
	for _, v := range meta.Videos {
		vurl := strings.TrimSpace(v.URL)
		if vurl == "" {
			continue
		}
		label := mapOkRuQuality(v.Name)
		if seen[label] {
			continue
		}
		seen[label] = true
		st.Qualities = append(st.Qualities, source.Quality{
			Label:  label,
			URL:    vurl,
			Height: Height(label),
		})
	}

	hlsURL := strings.TrimSpace(meta.HlsManifestURL)
	if hlsURL == "" {
		hlsURL = strings.TrimSpace(meta.OndemandHls)
	}
	if hlsURL != "" && len(st.Qualities) == 0 {
		st.Qualities = append(st.Qualities, source.Quality{
			Label: "auto",
			URL:   hlsURL,
		})
	}

	if len(st.Qualities) == 0 {
		kind := Classify(200, body)
		if kind == KindUnknown {
			kind = KindDeleted
		}
		return nil, &Error{Extractor: "OK.ru", Kind: kind, Detail: "ok.ru: oynatılabilir video bulunamadı"}
	}
	st.SortQualities()
	return st, nil
}

func mapOkRuQuality(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mobile":
		return "144p"
	case "lowest":
		return "240p"
	case "low":
		return "360p"
	case "sd":
		return "480p"
	case "hd":
		return "720p"
	case "full":
		return "1080p"
	case "quad":
		return "1440p"
	case "ultra":
		return "2160p"
	}
	if name == "" {
		return "auto"
	}
	return name
}
