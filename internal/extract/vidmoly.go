package extract

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

type vidmolyExtractor struct{}

func init() { Register(vidmolyExtractor{}) }

func (vidmolyExtractor) Name() string { return "Vidmoly" }

func (vidmolyExtractor) CanHandle(u *url.URL) bool {
	return u != nil && strings.Contains(strings.ToLower(u.Host), "vidmoly")
}

func (vidmolyExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return vidmolyResolve(ctx, u, hdr)
}

var (
	reVidmolySources = regexp.MustCompile(`sources:\s*\[\s*\{\s*file:\s*['"]([^'"]+)['"]`)
	reVidmolyFileAlt = regexp.MustCompile(`file:\s*['"]([^'"]+\.m3u8[^'"]*)['"]`)
	reVidmolyWatch   = regexp.MustCompile(`^/w/([A-Za-z0-9_-]+)`)
)

// Vidmoly resolves vidmoly.to / vidmoly.me embed pages into HLS streams.
func Vidmoly(ctx context.Context, embed string) (*source.Stream, error) {
	u, err := url.Parse(NormalizeURL(embed))
	if err != nil {
		return nil, err
	}
	return vidmolyResolve(ctx, u, nil)
}

func vidmolyResolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	origin := "https://vidmoly.to"
	if u.Scheme != "" && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}
	target := u.String()
	if m := reVidmolyWatch.FindStringSubmatch(u.Path); m != nil {
		target = origin + "/embed-" + m[1] + ".html"
	}

	reqHdr := map[string]string{"Referer": origin + "/"}
	for k, v := range hdr {
		reqHdr[k] = v
	}
	body, err := netx.Get(ctx, target, reqHdr)
	if err != nil {
		kind := KindOf(err)
		return nil, &Error{Extractor: "Vidmoly", Kind: kind, Detail: err.Error(), Err: err}
	}

	var m3u8URL string
	if m := reVidmolySources.FindSubmatch(body); m != nil {
		m3u8URL = strings.TrimSpace(string(m[1]))
	} else if m := reVidmolyFileAlt.FindSubmatch(body); m != nil {
		m3u8URL = strings.TrimSpace(string(m[1]))
	}
	if m3u8URL == "" {
		kind := Classify(200, body)
		if kind == KindUnknown {
			kind = KindChanged
		}
		return nil, &Error{Extractor: "Vidmoly", Kind: kind, Detail: "vidmoly: m3u8 adresi bulunamadı"}
	}
	if strings.HasPrefix(m3u8URL, "//") {
		m3u8URL = "https:" + m3u8URL
	}

	return &source.Stream{
		Referer:   origin + "/",
		UserAgent: netx.UserAgent,
		Qualities: []source.Quality{{Label: "auto", URL: m3u8URL}},
	}, nil
}
