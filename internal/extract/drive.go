package extract

import (
	"context"
	"net/url"
	"strings"

	"aniclitr/internal/source"
)

type driveExtractor struct{}

func init() { Register(driveExtractor{}) }

func (driveExtractor) Name() string { return "Google Drive" }

func (driveExtractor) CanHandle(u *url.URL) bool {
	return u != nil && strings.Contains(strings.ToLower(u.Host), "drive.google.com")
}

func (driveExtractor) Resolve(ctx context.Context, u *url.URL, _ map[string]string) (*source.Stream, error) {
	return Drive(ctx, u.String())
}

// Drive checks a Google Drive video with yt-dlp, then hands the page itself
// on: its stream links only work with cookies set while resolving, which a
// plain URL would lose.
func Drive(ctx context.Context, embed string) (*source.Stream, error) {
	st, err := YtDlp(ctx, embed)
	if err != nil {
		return nil, &Error{
			Extractor: "Google Drive",
			Kind:      KindOf(err),
			Detail:    err.Error(),
			Err:       err,
		}
	}
	h := st.Qualities[0].Height
	if u, err := url.Parse(embed); err == nil {
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/preview"), "/view") + "/view"
		embed = u.String()
	}
	return &source.Stream{
		Qualities: []source.Quality{{Label: labelFor(h), URL: embed, Height: h, Page: true}},
	}, nil
}
