package plugin

import (
	"encoding/base64"
	"encoding/hex"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	rePacker = regexp.MustCompile(`eval\(function\(p,a,c,k,e,[dr]\)\{[\s\S]*?\}\('([\s\S]*?)',\s*(\d+),\s*(\d+),\s*'([\s\S]*?)'\.split\('\|'\)`)
	reToken  = regexp.MustCompile(`\b[0-9a-zA-Z]+\b`)
)

// UnpackAll finds and unpacks every Dean Edwards P.A.C.K.E.R block in src,
// returning the concatenated unpacked JS (or empty string if none matched).
func UnpackAll(src string) string {
	matches := rePacker.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return ""
	}
	var out strings.Builder
	for _, m := range matches {
		if len(m) < 5 {
			continue
		}
		payload := unescapePackerQuotes(m[1])
		radix, err1 := strconv.Atoi(m[2])
		count, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || radix < 2 || radix > 62 || count < 0 || count > 65536 {
			continue
		}
		symtab := strings.Split(unescapePackerQuotes(m[4]), "|")
		if len(symtab) < count {
			padded := make([]string, count)
			copy(padded, symtab)
			symtab = padded
		}
		unpacked := unpackOne(payload, radix, symtab)
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(unpacked)
	}
	return out.String()
}

func unescapePackerQuotes(s string) string {
	s = strings.ReplaceAll(s, `\'`, `'`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func unpackOne(payload string, radix int, symtab []string) string {
	return reToken.ReplaceAllStringFunc(payload, func(tok string) string {
		idx, ok := parseBase(tok, radix)
		if !ok || idx < 0 || idx >= len(symtab) || symtab[idx] == "" {
			return tok
		}
		return symtab[idx]
	})
}

func parseBase(s string, radix int) (int, bool) {
	if s == "" || radix < 2 || radix > 62 {
		return 0, false
	}
	val := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		d := -1
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			if radix <= 36 {
				d = int(c-'A') + 10
			} else {
				d = int(c-'A') + 36
			}
		}
		if d < 0 || d >= radix {
			return 0, false
		}
		if val > 1<<20 {
			return 0, false
		}
		val = val*radix + d
	}
	return val, true
}

// rc4Crypt applies symmetric RC4 encryption/decryption.
func rc4Crypt(data, key []byte) []byte {
	if len(key) == 0 {
		return append([]byte(nil), data...)
	}
	var s [256]byte
	for i := range s {
		s[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + int(s[i]) + int(key[i%len(key)])) & 0xff
		s[i], s[j] = s[j], s[i]
	}
	out := make([]byte, len(data))
	a, b := 0, 0
	for i := 0; i < len(data); i++ {
		a = (a + 1) & 0xff
		b = (b + int(s[a])) & 0xff
		s[a], s[b] = s[b], s[a]
		out[i] = data[i] ^ s[(int(s[a])+int(s[b]))&0xff]
	}
	return out
}

func xorBytes(data, key []byte) []byte {
	if len(key) == 0 {
		return append([]byte(nil), data...)
	}
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ key[i%len(key)]
	}
	return out
}

func decodeBase64Flexible(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}

func decodeHexString(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(s))
}

func unescapeURL(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

func unescapeHTML(s string) string {
	return html.UnescapeString(s)
}

func unescapeJSONSlashes(s string) string {
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\u0026`, `&`)
	s = strings.ReplaceAll(s, `\u002F`, `/`)
	s = strings.ReplaceAll(s, `\u002f`, `/`)
	return s
}
