package plugin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aniclitr/internal/extract"
	"aniclitr/internal/netx"
	"aniclitr/internal/source"
)

// StepTrace captures detailed execution data for a single step in the
// Developer Resolver Lab.
type StepTrace struct {
	Index      int           `json:"index"`
	Op         string        `json:"op"`
	OK         bool          `json:"ok"`
	Skipped    bool          `json:"skipped,omitempty"`
	HTTPStatus int           `json:"http_status,omitempty"`
	Note       string        `json:"note,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// RunReport is the full execution and failure-diagnosis report returned by
// Pack.ResolveTrace (used by the Developer Lab).
type RunReport struct {
	Plugin       string         `json:"plugin"`
	URL          string         `json:"url"`
	Stream       *source.Stream `json:"stream,omitempty"`
	Err          error          `json:"-"`
	Diagnosis    string         `json:"diagnosis,omitempty"`
	Kind         extract.Kind   `json:"kind"`
	RedirectFrom string         `json:"redirect_from,omitempty"`
	RedirectTo   string         `json:"redirect_to,omitempty"`
	Traces       []StepTrace    `json:"traces"`
	Duration     time.Duration  `json:"duration"`
}

// Resolve implements extract.Extractor.
func (p *Pack) Resolve(ctx context.Context, u *url.URL, hdr map[string]string) (*source.Stream, error) {
	rep := p.ResolveTrace(ctx, u, hdr, nil, "")
	if rep.Stream != nil && len(rep.Stream.Qualities) > 0 {
		return rep.Stream, nil
	}
	if rep.Err != nil {
		return nil, rep.Err
	}
	return nil, &extract.Error{
		Extractor: p.Manifest.Name,
		Kind:      rep.Kind,
		Detail:    rep.Diagnosis,
	}
}

// ResolveTrace executes the manifest pipeline and produces a rich RunReport.
// If client is nil, netx.Client is used. If fixtureBody is non-empty, the
// first http_get step receives fixtureBody instead of making a network request
// (enabling fast offline isolated resolver tests in the Developer Lab).
func (p *Pack) ResolveTrace(ctx context.Context, u *url.URL, hdr map[string]string, client *http.Client, fixtureBody string) RunReport {
	t0 := time.Now()
	rep := RunReport{
		Plugin: p.Manifest.Name,
		URL:    u.String(),
	}
	if client == nil {
		client = netx.Client
	}

	vars := map[string]string{
		"url":    u.String(),
		"scheme": u.Scheme,
		"host":   u.Host,
		"origin": u.Scheme + "://" + u.Host,
		"path":   u.Path,
		"query":  u.RawQuery,
	}
	for k, v := range hdr {
		vars["hdr_"+strings.ToLower(k)] = v
	}

	st := &source.Stream{}
	usedFixture := false
	seenURL := map[string]bool{}
	addQuality := func(rawURL, label, ref, ua string, forceHLS bool) {
		rawURL = strings.TrimSpace(unescapeJSONSlashes(rawURL))
		if rawURL == "" {
			return
		}
		if strings.HasPrefix(rawURL, "//") {
			rawURL = "https:" + rawURL
		}
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			return
		}
		if seenURL[rawURL] {
			return
		}
		seenURL[rawURL] = true
		h := extract.Height(label)
		if label == "" {
			if h > 0 {
				label = strconv.Itoa(h) + "p"
			} else {
				label = "auto"
			}
		}
		st.Qualities = append(st.Qualities, source.Quality{
			Label:  label,
			URL:    rawURL,
			Height: h,
		})
		if st.Referer == "" && ref != "" {
			st.Referer = ref
		}
		if st.UserAgent == "" && ua != "" {
			st.UserAgent = ua
		}
	}

	var diagMsg string
	var diagKind extract.Kind

	for idx, step := range p.Manifest.Steps {
		s0 := time.Now()
		tr := StepTrace{Index: idx + 1, Op: step.Op}

		if step.OnlyIf != "" && strings.TrimSpace(vars[step.OnlyIf]) == "" {
			tr.OK, tr.Skipped, tr.Note, tr.Duration = true, true, "only_if boş: "+step.OnlyIf, time.Since(s0)
			rep.Traces = append(rep.Traces, tr)
			continue
		}
		if step.SkipIf != "" && strings.TrimSpace(vars[step.SkipIf]) != "" {
			tr.OK, tr.Skipped, tr.Note, tr.Duration = true, true, "skip_if dolu: "+step.SkipIf, time.Since(s0)
			rep.Traces = append(rep.Traces, tr)
			continue
		}

		fromVar := step.From
		if fromVar == "" {
			fromVar = "body"
		}

		switch step.Op {
		case "set":
			val := interpolate(step.Template, vars)
			if step.Var != "" {
				vars[step.Var] = val
			}
			tr.OK, tr.Note = true, fmt.Sprintf("%s=%s", step.Var, extract.Summarize(val, 60))

		case "url_replace":
			if idx >= len(p.stepRes) || p.stepRes[idx] == nil {
				tr.OK, tr.Note = step.Optional, "geçersiz regex"
				break
			}
			target := step.From
			if target == "" {
				target = "url"
			}
			in := vars[target]
			out := p.stepRes[idx].ReplaceAllString(in, interpolate(step.Replace, vars))
			dst := step.Var
			if dst == "" {
				dst = target
			}
			vars[dst] = out
			if dst == "url" {
				if nu, err := url.Parse(out); err == nil && nu.Host != "" {
					vars["scheme"], vars["host"], vars["origin"], vars["path"], vars["query"] = nu.Scheme, nu.Host, nu.Scheme+"://"+nu.Host, nu.Path, nu.RawQuery
				}
			}
			tr.OK, tr.Note = true, extract.Summarize(out, 80)

		case "http_get", "http_post":
			reqURL := interpolate(step.URL, vars)
			if strings.HasPrefix(reqURL, "//") {
				reqURL = "https:" + reqURL
			}
			if step.Op == "http_get" && fixtureBody != "" && !usedFixture {
				usedFixture = true
				dst := step.Var
				if dst == "" {
					dst = "body"
				}
				vars[dst] = fixtureBody
				vars["final_url"] = reqURL
				vars["status"] = "200"
				tr.OK, tr.HTTPStatus, tr.Note = true, 200, fmt.Sprintf("fixture (%d bayt)", len(fixtureBody))
				break
			}
			body, status, finalURL, loc, respHdr, err := doHTTP(ctx, client, step, reqURL, vars)
			tr.HTTPStatus = status
			if err != nil {
				tr.OK = false
				tr.Note = err.Error()
				if !step.Optional {
					diagKind = extract.KindOf(err)
					diagMsg = fmt.Sprintf("adım #%d (%s) ağ hatası: %v", idx+1, step.Op, err)
				}
				break
			}
			// Detect cross-domain redirect
			if origU, err1 := url.Parse(reqURL); err1 == nil {
				checkTarget := finalURL
				if loc != "" {
					checkTarget = loc
				}
				if newU, err2 := url.Parse(checkTarget); err2 == nil && newU.Host != "" && !strings.EqualFold(origU.Hostname(), newU.Hostname()) {
					rep.RedirectFrom = origU.Hostname()
					rep.RedirectTo = newU.Hostname()
					tr.Note = fmt.Sprintf("yönlendirme %s → %s", origU.Hostname(), newU.Hostname())
				}
			}
			if loc != "" {
				vars["location"] = loc
			}
			vars["final_url"] = finalURL
			vars["status"] = strconv.Itoa(status)
			for hk, hv := range respHdr {
				vars["resphdr_"+strings.ToLower(hk)] = hv
			}
			if status >= 400 {
				kind := extract.Classify(status, []byte(body))
				tr.OK = false
				switch {
				case status == 403 && step.Headers["Referer"] != "":
					tr.Note = "Referer reddedildi (HTTP 403)"
				case status == 403 && kind == extract.KindBlocked:
					tr.Note = "Cloudflare / bot duvarı (HTTP 403)"
				case status == 404 || status == 410:
					tr.Note = fmt.Sprintf("video kaldırılmış / bulunamadı (HTTP %d)", status)
				default:
					tr.Note = fmt.Sprintf("HTTP %d", status)
				}
				if !step.Optional {
					diagKind = kind
					diagMsg = tr.Note
				}
				break
			}
			dst := step.Var
			if dst == "" {
				dst = "body"
			}
			vars[dst] = body
			tr.OK = true
			if tr.Note == "" {
				tr.Note = fmt.Sprintf("HTTP %d (%d bayt)", status, len(body))
			}

		case "js_unpack":
			in := vars[fromVar]
			un := UnpackAll(in)
			dst := step.Var
			if dst == "" {
				dst = "unpacked"
			}
			if un == "" {
				if step.Optional {
					tr.OK, tr.Note = true, "packer yok (opsiyonel)"
				} else {
					tr.OK = false
					tr.Note = "JS packer (eval(p,a,c,k,e,d)) bulunamadı veya yapısı değişti"
					diagKind = extract.KindChanged
					diagMsg = tr.Note
				}
			} else {
				vars[dst] = un
				tr.OK, tr.Note = true, fmt.Sprintf("çözüldü (%d bayt)", len(un))
			}

		case "regex":
			if idx >= len(p.stepRes) || p.stepRes[idx] == nil {
				tr.OK, tr.Note = step.Optional, "geçersiz regex"
				break
			}
			in := vars[fromVar]
			m := p.stepRes[idx].FindStringSubmatch(in)
			if len(m) == 0 {
				if step.Optional {
					tr.OK, tr.Note = true, "eşleşmedi (opsiyonel)"
				} else {
					tr.OK = false
					tr.Note = fmt.Sprintf("adım #%d regex eşleşmedi (HTML/JSON yapısı değişmiş olabilir)", idx+1)
					diagKind = extract.KindChanged
					diagMsg = tr.Note
				}
				break
			}
			if len(step.Vars) > 0 {
				for gi, vn := range step.Vars {
					if gi+1 < len(m) && vn != "" {
						vars[vn] = m[gi+1]
					}
				}
			} else if step.Var != "" {
				if len(m) > 1 {
					vars[step.Var] = m[1]
				} else {
					vars[step.Var] = m[0]
				}
			}
			tr.OK = true
			if len(m) > 1 {
				tr.Note = extract.Summarize(m[1], 70)
			} else {
				tr.Note = extract.Summarize(m[0], 70)
			}

		case "base64_decode":
			b, err := decodeBase64Flexible(vars[fromVar])
			if err != nil {
				tr.OK, tr.Note = step.Optional, "base64 çözülemedi: "+err.Error()
				if !step.Optional {
					diagKind, diagMsg = extract.KindChanged, tr.Note
				}
				break
			}
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = string(b)
			tr.OK, tr.Note = true, fmt.Sprintf("%d bayt", len(b))

		case "hex_decode":
			b, err := decodeHexString(vars[fromVar])
			if err != nil {
				tr.OK, tr.Note = step.Optional, "hex çözülemedi: "+err.Error()
				if !step.Optional {
					diagKind, diagMsg = extract.KindChanged, tr.Note
				}
				break
			}
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = string(b)
			tr.OK, tr.Note = true, fmt.Sprintf("%d bayt", len(b))

		case "url_unescape":
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = unescapeURL(vars[fromVar])
			tr.OK = true

		case "html_unescape":
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = unescapeHTML(vars[fromVar])
			tr.OK = true

		case "json_unescape":
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = unescapeJSONSlashes(vars[fromVar])
			tr.OK = true

		case "xor_decode":
			key := interpolate(step.Key, vars)
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = string(xorBytes([]byte(vars[fromVar]), []byte(key)))
			tr.OK = true

		case "rc4_decrypt":
			key := interpolate(step.Key, vars)
			dst := step.Var
			if dst == "" {
				dst = fromVar
			}
			vars[dst] = string(rc4Crypt([]byte(vars[fromVar]), []byte(key)))
			tr.OK = true

		case "json_extract":
			in := vars[fromVar]
			ref := interpolate(step.Referer, vars)
			ua := interpolate(step.UserAgent, vars)
			items, err := extractJSONItems(in, step.Path, step.URLField, step.LabelField)
			if err != nil {
				tr.OK, tr.Note = step.Optional, "JSON ayrıştırılamadı: "+err.Error()
				if !step.Optional {
					diagKind, diagMsg = extract.KindChanged, tr.Note
				}
				break
			}
			for _, it := range items {
				if step.Var != "" {
					vars[step.Var] = it.url
				}
				addQuality(it.url, it.label, ref, ua, step.HLS)
			}
			if len(items) == 0 && !step.Optional {
				tr.OK, tr.Note = false, fmt.Sprintf("JSON yolu %q boş döndü (API yapısı değişmiş olabilir)", step.Path)
				diagKind, diagMsg = extract.KindChanged, tr.Note
			} else {
				tr.OK, tr.Note = true, fmt.Sprintf("%d akış bulundu", len(items))
			}

		case "emit":
			uStr := interpolate(step.URL, vars)
			lbl := interpolate(step.Label, vars)
			ref := interpolate(step.Referer, vars)
			ua := interpolate(step.UserAgent, vars)
			before := len(st.Qualities)
			addQuality(uStr, lbl, ref, ua, step.HLS)
			if len(st.Qualities) == before && !step.Optional {
				tr.OK, tr.Note = false, "geçerli akış URL'si üretilemedi"
				diagKind, diagMsg = extract.KindChanged, tr.Note
			} else {
				tr.OK, tr.Note = true, extract.Summarize(uStr, 70)
			}

		case "emit_regex_all":
			if idx >= len(p.stepRes) || p.stepRes[idx] == nil {
				tr.OK, tr.Note = step.Optional, "geçersiz regex"
				break
			}
			in := vars[fromVar]
			ref := interpolate(step.Referer, vars)
			ua := interpolate(step.UserAgent, vars)
			ug := step.URLGroup
			if ug <= 0 {
				ug = 1
			}
			matches := p.stepRes[idx].FindAllStringSubmatch(in, -1)
			added := 0
			for _, m := range matches {
				if ug >= len(m) {
					continue
				}
				lbl := interpolate(step.Label, vars)
				if step.LabelGroup > 0 && step.LabelGroup < len(m) && m[step.LabelGroup] != "" {
					lbl = m[step.LabelGroup]
				}
				before := len(st.Qualities)
				addQuality(m[ug], lbl, ref, ua, step.HLS)
				if len(st.Qualities) > before {
					added++
				}
			}
			if added == 0 && !step.Optional {
				tr.OK, tr.Note = false, fmt.Sprintf("adım #%d regex akış bulamadı", idx+1)
				diagKind, diagMsg = extract.KindChanged, tr.Note
			} else {
				tr.OK, tr.Note = true, fmt.Sprintf("%d akış bulundu", added)
			}

		case "mirror_retry":
			if len(st.Qualities) > 0 || len(p.Manifest.Mirrors) == 0 || p.Manifest.MirrorTemplate == "" {
				tr.OK, tr.Skipped, tr.Note = true, true, "gerekmedi"
				break
			}
			tried := 0
			for _, mir := range p.Manifest.Mirrors {
				vars["mirror"] = mir
				mURL := interpolate(p.Manifest.MirrorTemplate, vars)
				body, status, _, _, _, err := doHTTP(ctx, client, Step{Op: "http_get", URL: mURL, Headers: step.Headers}, mURL, vars)
				tried++
				if err != nil || status >= 400 || body == "" {
					continue
				}
				un := UnpackAll(body)
				searchIn := body
				if un != "" {
					searchIn = un + "\n" + body
				}
				if p.stepRes[idx] != nil {
					if m := p.stepRes[idx].FindStringSubmatch(searchIn); len(m) > 1 {
						addQuality(m[1], step.Label, "https://"+mir+"/", interpolate(step.UserAgent, vars), step.HLS)
						break
					}
				}
			}
			if len(st.Qualities) > 0 {
				tr.OK, tr.Note = true, fmt.Sprintf("%d ayna denendi, akış bulundu", tried)
			} else {
				tr.OK, tr.Note = step.Optional, fmt.Sprintf("%d ayna denendi, sonuç yok", tried)
			}
		}

		tr.Duration = time.Since(s0)
		rep.Traces = append(rep.Traces, tr)
		if !tr.OK && !step.Optional {
			break
		}
	}

	rep.Duration = time.Since(t0)
	if len(st.Qualities) > 0 {
		st.SortQualities()
		rep.Stream = st
		rep.Diagnosis = fmt.Sprintf("çözüldü (%d kalite)", len(st.Qualities))
		return rep
	}

	// Build rich human-readable Turkish root-cause diagnosis.
	if diagKind == extract.KindUnknown {
		if b := vars["body"]; b != "" {
			diagKind = extract.Classify(atoiDefault(vars["status"], 0), []byte(b))
		}
	}
	if diagMsg == "" {
		switch {
		case rep.RedirectFrom != "" && rep.RedirectTo != "":
			diagKind = extract.KindChanged
			diagMsg = fmt.Sprintf("domain yönlendirdi (%s → %s) ve akış bulunamadı", rep.RedirectFrom, rep.RedirectTo)
		case diagKind != extract.KindUnknown:
			diagMsg = diagKind.Message()
		default:
			diagKind = extract.KindChanged
			diagMsg = "oynatılabilir akış bulunamadı (sayfa yapısı değişmiş olabilir)"
		}
	} else if rep.RedirectFrom != "" && rep.RedirectTo != "" && !strings.Contains(diagMsg, "yönlendirdi") {
		diagMsg = fmt.Sprintf("domain yönlendirdi (%s → %s) · %s", rep.RedirectFrom, rep.RedirectTo, diagMsg)
	}
	rep.Kind = diagKind
	rep.Diagnosis = diagMsg
	rep.Err = &extract.Error{
		Extractor: p.Manifest.Name,
		Kind:      diagKind,
		Detail:    diagMsg,
	}
	return rep
}

func doHTTP(ctx context.Context, client *http.Client, step Step, reqURL string, vars map[string]string) (string, int, string, string, map[string]string, error) {
	method := http.MethodGet
	var bodyReader io.Reader
	if step.Op == "http_post" {
		method = http.MethodPost
		if len(step.Form) > 0 {
			vals := url.Values{}
			for k, v := range step.Form {
				vals.Set(k, interpolate(v, vars))
			}
			bodyReader = strings.NewReader(vals.Encode())
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return "", 0, reqURL, "", nil, err
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	if step.Op == "http_post" && len(step.Form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	}
	for k, v := range step.Headers {
		val := interpolate(v, vars)
		if val != "" {
			req.Header.Set(k, val)
		}
	}

	c := client
	if step.NoFollow {
		cloned := *client
		cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		c = &cloned
	}

	resp, err := c.Do(req)
	if err != nil {
		return "", 0, reqURL, "", nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", resp.StatusCode, reqURL, "", nil, err
	}
	finalURL := reqURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	loc := resp.Header.Get("Location")
	if loc != "" && resp.Request != nil && resp.Request.URL != nil {
		if rel, err := url.Parse(loc); err == nil {
			loc = resp.Request.URL.ResolveReference(rel).String()
		}
	}
	hdrOut := map[string]string{}
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			hdrOut[k] = vs[0]
		}
	}
	return string(b), resp.StatusCode, finalURL, loc, hdrOut, nil
}

var reVar = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

func interpolate(tmpl string, vars map[string]string) string {
	if !strings.Contains(tmpl, "{{") {
		return tmpl
	}
	return reVar.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		switch {
		case key == "now_ms":
			return strconv.FormatInt(time.Now().UnixMilli(), 10)
		case key == "now_sec":
			return strconv.FormatInt(time.Now().Unix(), 10)
		case strings.HasPrefix(key, "rand:"):
			n, _ := strconv.Atoi(strings.TrimPrefix(key, "rand:"))
			if n <= 0 || n > 64 {
				n = 10
			}
			return randomAlnum(n)
		case strings.HasPrefix(key, "urlescape:"):
			return url.QueryEscape(vars[strings.TrimPrefix(key, "urlescape:")])
		default:
			return vars[key]
		}
	})
}

func randomAlnum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

type jsonStreamItem struct {
	url   string
	label string
}

// extractJSONItems walks a dot-separated path supporting "*" (map wildcard)
// and "[]" (array fan-out). Examples:
//   - "videos[]" with urlField="url", labelField="key"
//   - "resources.*.videoStreams.videos[]" with urlField="url", labelField="dimension"
//   - "qualities.auto[]" with urlField="url"
//   - "" (root object whose map entries are quality -> url, e.g., VK files JSON)
func extractJSONItems(raw, path, urlField, labelField string) ([]jsonStreamItem, error) {
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, err
	}
	nodes := []any{root}
	if strings.TrimSpace(path) != "" {
		segs := strings.Split(path, ".")
		for _, seg := range segs {
			fanArray := strings.HasSuffix(seg, "[]")
			key := strings.TrimSuffix(seg, "[]")
			var next []any
			for _, n := range nodes {
				if key == "*" {
					if mp, ok := n.(map[string]any); ok {
						for _, v := range mp {
							next = append(next, v)
						}
					}
				} else if key != "" {
					if mp, ok := n.(map[string]any); ok {
						if v, exists := mp[key]; exists {
							next = append(next, v)
						}
					}
				} else {
					next = append(next, n)
				}
			}
			if fanArray {
				var flat []any
				for _, n := range next {
					if arr, ok := n.([]any); ok {
						flat = append(flat, arr...)
					}
				}
				next = flat
			}
			nodes = next
		}
	}

	var out []jsonStreamItem
	for _, n := range nodes {
		switch v := n.(type) {
		case string:
			if strings.HasPrefix(v, "http") || strings.HasPrefix(v, "//") {
				out = append(out, jsonStreamItem{url: v})
			}
		case map[string]any:
			if urlField == "" {
				// Map of qualityKey -> urlString (e.g. VK mp4_720, hls)
				for k, mv := range v {
					if s, ok := mv.(string); ok && (strings.HasPrefix(s, "http") || strings.HasPrefix(s, "//")) {
						lbl := k
						if strings.HasPrefix(k, "mp4_") {
							lbl = strings.TrimPrefix(k, "mp4_") + "p"
						} else if strings.Contains(k, "hls") {
							lbl = "auto"
						}
						out = append(out, jsonStreamItem{url: s, label: lbl})
					}
				}
				continue
			}
			uVal, _ := v[urlField].(string)
			if uVal == "" {
				continue
			}
			lbl := ""
			if labelField != "" {
				if ls, ok := v[labelField].(string); ok {
					lbl = ls
				} else if lf, ok := v[labelField].(float64); ok {
					lbl = strconv.Itoa(int(lf)) + "p"
				}
			}
			out = append(out, jsonStreamItem{url: uVal, label: lbl})
		}
	}
	return out, nil
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}
