package extract

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

type sibnetExtractor struct{}

func init() { Register(sibnetExtractor{}) }

func (sibnetExtractor) Name() string { return "Sibnet" }

func (sibnetExtractor) CanHandle(u *url.URL) bool {
	return u != nil && strings.Contains(strings.ToLower(u.Host), "sibnet")
}

func (sibnetExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return sibnetResolve(ctx, u, hdr)
}

var (
	sibnetSrc     = regexp.MustCompile(`(?:player\.src\(\[\{src:\s*"|src:\s*")([^"]+\.mp4[^"]*)"`)
	sibnetPathID  = regexp.MustCompile(`/video(\d+)`)
	sibnetParamID = regexp.MustCompile(`videoid=(\d+)`)
)

// Sibnet scrapes the mp4 path from the Sibnet shell page.
func Sibnet(ctx context.Context, embed string) (*source.Stream, error) {
	u, err := url.Parse(NormalizeURL(embed))
	if err != nil {
		return nil, err
	}
	return sibnetResolve(ctx, u, nil)
}

func sibnetResolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	origin := "https://video.sibnet.ru"
	if u.Scheme != "" && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}
	shellURL := u.String()
	if !sibnetParamID.MatchString(shellURL) {
		if m := sibnetPathID.FindStringSubmatch(u.Path); m != nil {
			shellURL = origin + "/shell.php?videoid=" + m[1]
		}
	}
	reqHdr := map[string]string{"Referer": origin + "/"}
	for k, v := range hdr {
		reqHdr[k] = v
	}
	body, err := netx.Get(ctx, shellURL, reqHdr)
	if err != nil {
		kind := KindOf(err)
		return nil, &Error{Extractor: "Sibnet", Kind: kind, Detail: err.Error(), Err: err}
	}
	m := sibnetSrc.FindSubmatch(body)
	if m == nil {
		kind := Classify(200, body)
		if kind == KindUnknown {
			kind = KindChanged
		}
		return nil, &Error{Extractor: "Sibnet", Kind: kind, Detail: "sibnet: kaynak bulunamadı"}
	}
	src := string(m[1])
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	} else if strings.HasPrefix(src, "/") {
		src = origin + src
	}
	return &source.Stream{
		Referer:   shellURL,
		Qualities: []source.Quality{{Label: "auto", URL: src}},
	}, nil
}
