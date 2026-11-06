package extract

import (
	"context"
	"net/url"
	"strings"

	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

type tauExtractor struct{}

func init() { Register(tauExtractor{}) }

func (tauExtractor) Name() string { return "Tau Video" }

func (tauExtractor) CanHandle(u *url.URL) bool {
	return u != nil && strings.Contains(strings.ToLower(u.Host), "tau-video")
}

func (tauExtractor) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	return tauResolve(ctx, u, hdr)
}

// Tau resolves tau-video.xyz embeds through its public JSON API.
func Tau(ctx context.Context, u *url.URL) (*source.Stream, error) {
	return tauResolve(ctx, u, nil)
}

func tauResolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := parts[len(parts)-1]
	if id == "" || id == "embed" || id == "video" {
		return nil, &Error{Extractor: "Tau Video", Kind: KindChanged, Detail: "tau: embed id yok"}
	}
	origin := "https://tau-video.xyz"
	if u.Scheme != "" && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}
	api := origin + "/api/video/" + id
	if vid := u.Query().Get("vid"); vid != "" {
		api += "?vid=" + url.QueryEscape(vid)
	}
	reqHdr := map[string]string{"Referer": u.String()}
	for k, v := range hdr {
		reqHdr[k] = v
	}
	var res struct {
		URLs []struct {
			Label string `json:"label"`
			URL   string `json:"url"`
			Size  int64  `json:"size"`
		} `json:"urls"`
		Message string `json:"message"`
	}
	if err := netx.GetJSON(ctx, api, reqHdr, &res); err != nil {
		kind := KindOf(err)
		if kind == KindNotFound {
			kind = KindDeleted
		}
		return nil, &Error{Extractor: "Tau Video", Kind: kind, Detail: err.Error(), Err: err}
	}
	st := &source.Stream{Referer: origin + "/"}
	seen := map[string]bool{}
	for _, x := range res.URLs {
		if x.URL == "" || seen[x.Label] {
			continue
		}
		seen[x.Label] = true
		st.Qualities = append(st.Qualities, source.Quality{
			Label:  x.Label,
			URL:    x.URL,
			Height: Height(x.Label),
			Size:   x.Size,
		})
	}
	if len(st.Qualities) == 0 {
		kind := Classify(200, []byte(res.Message))
		if kind == KindUnknown {
			kind = KindDeleted
		}
		return nil, &Error{Extractor: "Tau Video", Kind: kind, Detail: "tau: video yok"}
	}
	st.SortQualities()
	return st, nil
}
