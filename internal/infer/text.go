// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// What structpeek can tell about a string by looking at it.

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	dateRE  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRE  = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d+)?([zZ]|[+-]\d{2}:\d{2})$`)
	emailRE = regexp.MustCompile(`^[A-Za-z0-9.!#$%&'*+=?^_{|}~-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$`)
	intRE   = regexp.MustCompile(`^-?[0-9]+$`)
)

// format returns the JSON Schema format s is written in, if it's one
// structpeek recognises, or "".
func format(s string) string {
	switch {
	case uuidRE.MatchString(s):
		return "uuid"
	case dateRE.MatchString(s):
		if _, err := time.Parse(time.DateOnly, s); err == nil {
			return "date"
		}
	case timeRE.MatchString(s):
		return "time"
	case emailRE.MatchString(s):
		return "email"
	}
	if len(s) >= 20 && s[4] == '-' {
		if _, err := time.Parse(time.RFC3339Nano, s); err == nil && strings.ContainsAny(s[10:11], "Tt") {
			return "date-time"
		}
	}
	if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" {
		if a.Is4() {
			return "ipv4"
		}
		return "ipv6"
	}
	if strings.Contains(s, "://") && !strings.ContainsFunc(s, unicode.IsSpace) {
		if u, err := url.Parse(s); err == nil && u.Scheme != "" {
			return "uri"
		}
	}
	return ""
}

// commonFormat returns the format every value shares, or "".
func commonFormat(vals []string) string {
	f := ""
	for _, v := range vals {
		g := format(v)
		if g == "" || f != "" && g != f {
			return ""
		}
		f = g
	}
	return f
}

// credentialRE matches strings that announce themselves as secrets: key
// blocks, tokens with well-known prefixes, and URLs with passwords in.
var credentialRE = regexp.MustCompile(strings.Join([]string{
	`-----BEGIN `,
	`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.`, // JWT
	`^(gh[pousr]_|github_pat_|glpat-|xox[abposr]-|sk-|sk_live_|sk_test_|rk_live_|rk_test_|whsec_|hf_|npm_|pypi-|shp(at|ss|ca|pa)_|dop_v1_|SG\.|AIza|ya29\.)[A-Za-z0-9_\-.]{10,}`,
	`^(AKIA|ASIA|AGPA|AIDA|AROA)[A-Z0-9]{16}$`,
	`(?i)^(bearer|basic|token)\s+\S{8,}`,
	`^[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]*:[^/\s@]+@`,
}, "|"))

// credential reports whether s looks like a secret by its shape.
func credential(s string) bool {
	return credentialRE.MatchString(s)
}

// highEntropy reports whether s is, or holds, something that looks
// randomly generated: a key, token, hash or the like. Each run of
// characters between separators is judged on its own, so a token inside a
// URL or a header still counts.
func highEntropy(s string) bool {
	if credential(s) {
		return true
	}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`/:?&=@,;"'()[]{}<>|\`, r)
	}) {
		if randomLooking(tok) {
			return true
		}
	}
	return false
}

// randomLooking judges one token. Random strings switch between lower
// case, upper case and digits all the way through, without the runs of
// lower case that make up words, and use their alphabet about as evenly
// as chance allows; words, identifiers, paths and version numbers don't.
func randomLooking(t string) bool {
	if len(t) < 16 {
		return false
	}
	var lower, upper, digits, flips, upperPairs, lowerRuns, vowels int
	consonants, clusters := 0, 0 // the current run of lower-case consonants, and runs of four or more
	hexLower, hexUpper := true, true
	prev := -1
	for i := 0; i < len(t); i++ {
		c, class := t[i], 0
		switch {
		case c >= 'a' && c <= 'z':
			lower++
			hexLower = hexLower && c <= 'f'
			hexUpper = false
			if prev != 0 {
				lowerRuns++
			}
			if strings.IndexByte("aeiouy", c) >= 0 {
				vowels++
				consonants = 0
			} else if consonants++; consonants == 4 {
				clusters++
			}
		case c >= 'A' && c <= 'Z':
			class = 1
			upper++
			consonants = 0
			hexUpper = hexUpper && c <= 'F'
			hexLower = false
			if prev == 1 {
				upperPairs++
			}
		case c >= '0' && c <= '9':
			class = 2
			digits++
			consonants = 0
		case strings.IndexByte("-_+.~", c) >= 0:
			consonants = 0
			continue // separators within a token, so they don't count
		default:
			return false
		}
		if prev >= 0 && class != prev {
			flips++
		}
		prev = class
	}
	n := float64(len(t))
	// Mixed case words come in runs of lower case with vowels in and
	// without long clusters of consonants, each begun by a capital that's
	// seldom followed by another (as in acronyms).
	wordy := upperPairs*3 <= upper && 2*lower > 5*lowerRuns && vowels*4 >= lower &&
		clusters == 0 && digits*8 < len(t)
	var alphabet float64
	switch {
	case (hexLower || hexUpper) && digits > 0:
		alphabet = 16
		if float64(flips) < n/8 {
			return false
		}
	case lower > 0 && upper > 0:
		alphabet = 64
		if wordy || float64(flips) < n/4 {
			return false
		}
	case digits > 0 && lower+upper > 0:
		alphabet = 36
		if float64(flips) < n/5 {
			return false
		}
	default:
		return false
	}
	// The entropy a random string this long would have, give or take.
	expected := math.Min(math.Log2(alphabet)-(alphabet-1)/(2*n*math.Ln2), math.Log2(n))
	return shannon(t) >= 0.8*expected
}

// opaque reports whether s has a long run of characters that switches
// between lower case, upper case and digits more than words do. It's
// a blunter test than highEntropy, for keeping values out of enums.
func opaque(s string) bool {
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_+.~", r))
	}) {
		if len(tok) < 16 {
			continue
		}
		flips, prev := 0, -1
		for i := 0; i < len(tok); i++ {
			class := 0
			switch c := tok[i]; {
			case c >= 'A' && c <= 'Z':
				class = 1
			case c >= '0' && c <= '9':
				class = 2
			case c < 'a' || c > 'z':
				continue
			}
			if prev >= 0 && class != prev {
				flips++
			}
			prev = class
		}
		if flips >= 4 {
			return true
		}
	}
	return false
}

// shannon returns the Shannon entropy of s, in bits per byte.
func shannon(s string) float64 {
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	h, n := 0.0, float64(len(s))
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// dataKey reports whether an object key looks like data rather than a
// field name: a number, an ID, an address, a date or a token.
func dataKey(k string) bool {
	return intRE.MatchString(k) || format(k) != "" || highEntropy(k)
}

// sensitiveWords are words that, in a key, mean its value is a secret or
// personal.
var sensitiveWords = map[string]bool{
	"password": true, "passwd": true, "passphrase": true, "pass": true, "pwd": true,
	"secret": true, "secrets": true, "token": true, "tokens": true, "apikey": true,
	"auth": true, "authorization": true, "credential": true, "credentials": true,
	"creds": true, "cookie": true, "cookies": true, "session": true, "sessionid": true,
	"sid": true, "signature": true, "sig": true, "salt": true, "hash": true,
	"pin": true, "ssn": true, "otp": true, "totp": true, "mfa": true, "private": true,
	"privkey": true, "key": true, "keys": true, "cert": true, "certificate": true,
	"bearer": true, "jwt": true, "nonce": true, "dsn": true, "iban": true,
	"cvv": true, "cvc": true, "card": true,
}

// sensitiveParts are spellings that mean a secret even run into other
// words, as in "dbpassword" or "githubtoken".
var sensitiveParts = []string{"passw", "secret", "token", "apikey", "privatekey", "accesskey", "credential"}

// sensitiveName reports whether a key's name says its value is a secret.
func sensitiveName(k string) bool {
	lower := strings.ToLower(k)
	for _, p := range sensitiveParts {
		if strings.Contains(lower, p) {
			return true
		}
	}
	for _, w := range words(k) {
		if sensitiveWords[w] {
			return true
		}
	}
	return false
}

// words splits a key like "apiKey", "api_key" or "API-Key" into
// lower-case words.
func words(k string) []string {
	var out []string
	var cur []rune
	rs := []rune(k)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
			continue
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(rs[i-1]) ||
			unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])):
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return out
}
