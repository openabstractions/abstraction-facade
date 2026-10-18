// Package redact owns how OA shows text a provider or engine printed: one
// line, with terminal escapes and invisible characters gone and anything
// shaped like a credential replaced by [redacted]. Every place that carries
// such text beyond the provider's own log (an exit reason, a refused check,
// a relayed reply) passes it through Line.
package redact

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Placeholder replaces each redacted value.
const Placeholder = "[redacted]"

// Line is text as OA may show it: terminal escape sequences, control
// characters, invalid bytes and Unicode separators each become a space,
// format characters (Unicode Cf: bidi overrides, zero-width characters) are
// removed, runs of spaces become one, and credential-shaped values are
// redacted. A positive limit then cuts the result to at most limit bytes on
// a character boundary.
//
// Redacted are: the whole value of an Authorization, Proxy-Authorization,
// Cookie or Set-Cookie header, also "Authorization Basic ..." with no
// separator; a bearer credential; the value of a field (name: value,
// name=value, name => value, <name>value), flag or phrase (name is value)
// whose name is a secretName, quoted (also with escaped quotes) or not, with
// the credential after an auth scheme word; URL user information; a JWT;
// keys with a known prefix (hf_, sk-, ghp_ and the other GitHub forms,
// glpat-, xox?-); AWS access key ids; and any run of 32 or more base64, hex
// or percent-encoded characters that holds a digit and 16 or more
// characters without a separator.
//
// Kept readable: a count of at most eight digits after a name whose only
// secret word is key or token (tokens: 4096), and a sha256:<64 hex> digest.
// Every such choice is a row of redacttest.Cases.
func Line(text string, limit int) string {
	text = normalize(text)
	text = redactValues(text, authorizationName, valueRest)
	text = redactValues(text, authorizationBare, valueSchemeRest)
	text = urlCredentials.ReplaceAllString(text, "${1}"+Placeholder+"@")
	text = redactValues(text, flagName, valueWord)
	text = redactValues(text, fieldName, valueWord)
	text = redactValues(text, phraseName, valuePhrase)
	text = redactValues(text, bearerName, valueWord)
	text = prefixedKey.ReplaceAllString(text, "${1}"+Placeholder)
	text = jwt.ReplaceAllString(text, Placeholder)
	text = awsKeyID.ReplaceAllString(text, Placeholder)
	text = redactLongTokens(text)
	if limit > 0 && len(text) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = strings.TrimSpace(text[:cut])
	}
	return text
}

// Escape sequences a terminal reads: CSI (ESC [ ... final), OSC (ESC ] ...
// BEL or ST) and any other two-byte escape.
var terminalEscape = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-_]")

// normalize makes text one line of visible characters. A removed control or
// escape leaves a space, so it never joins a name to the value after it.
func normalize(text string) string {
	text = terminalEscape.ReplaceAllString(text, " ")
	var b strings.Builder
	b.Grow(len(text))
	space := true
	for i, r := range text {
		switch {
		case r == utf8.RuneError && !strings.HasPrefix(text[i:], "\ufffd"),
			unicode.IsControl(r), unicode.IsSpace(r),
			unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp):
			if !space {
				b.WriteByte(' ')
				space = true
			}
		case unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
			space = false
		}
	}
	return strings.TrimSpace(b.String())
}

// secretName is a name whose value is redacted: one holding token, key,
// secret, password, passwd, passphrase, credential or signature anywhere,
// or auth, pass, pw, pwd or sig as a whole part of it (x-auth, db_pwd,
// --pass, sig; never author, bypass or signal).
const secretName = `(?:[\w.-]*(?:token|key|secret|passw(?:or)?d|passphrase|credential|signature)[\w.-]*|(?:[\w.-]*[_.-])?(?:auth|pass|pwd|pw|sig)(?:[_.-][\w.-]*)?\b)`

// strongWords mark a name as a credential's own. A name holding only key or
// token (tokens, monkey, primary key) lets a short count through: weakName.
var strongWords = []string{"secret", "pass", "credential", "auth", "pw", "sig"}

// weakName is a matched name whose only secret word is key or token.
func weakName(name string) bool {
	name = strings.ToLower(name)
	for _, w := range strongWords {
		if strings.Contains(name, w) {
			return false
		}
	}
	return true
}

// shortCount is an all-digit value of at most eight digits: a count or a
// small id, never a credential's whole value.
func shortCount(value string) bool {
	if value == "" || len(value) > 8 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Names each followed by the value redactValues replaces. A name may close
// its own quote, escaped or not, before the separator; > separates an XML
// element's name from its text.
var (
	authorizationName = regexp.MustCompile(`(?i)\b(?:(?:proxy-)?authorization|(?:set-)?cookie)(?:\\?["'])?\s*[:=]\s*`)
	// "Authorization Basic <credential>" with no separator: redacted when
	// the word after the name is an auth scheme (valueSchemeRest).
	authorizationBare = regexp.MustCompile(`(?i)\b(?:proxy-)?authorization\s+`)
	flagName          = regexp.MustCompile(`(?i)(?:^|\s)--?` + secretName + `(?:\s*=\s*|\s+)`)
	// => is one separator: a name never ends on its =, which would leave
	// only > before the value.
	fieldName  = regexp.MustCompile(`(?i)\b` + secretName + `(?:\\?["'])?\s*(?:=>|[:=>])\s*`)
	phraseName = regexp.MustCompile(`(?i)\b` + secretName + `\s+(?:is|was|are)(?:\s*:)?\s+`)
	bearerName = regexp.MustCompile(`(?i)\bbearer\b[\s:=]*|\bbearer`)
)

// authSchemes are words that open an Authorization value; a field whose value
// starts with one has the credential after it redacted too.
var authSchemes = map[string]bool{"bearer": true, "basic": true, "digest": true, "token": true, "negotiate": true}

var (
	urlCredentials = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	prefixedKey    = regexp.MustCompile(`\b(hf_|sk-|gh[pousr]_|github_pat_|glpat-|xox[abprs]-)[A-Za-z0-9_\-]+`)
	jwt            = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*`)
	awsKeyID       = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	longToken      = regexp.MustCompile(`[A-Za-z0-9+/=_%-]{32,}`)
)

// digestPrefix ends the text before a content digest: sha256:<64 hex> names
// bytes, not a credential, and stays readable.
var digestPrefix = regexp.MustCompile(`(?i)\bsha256:$`)

// redactLongTokens replaces each secretShaped run, except a 64-hex digest
// right after sha256:.
func redactLongTokens(text string) string {
	var b strings.Builder
	last := 0
	for _, m := range longToken.FindAllStringIndex(text, -1) {
		run := text[m[0]:m[1]]
		if !secretShaped(run) || len(run) == 64 && isHex(run) && digestPrefix.MatchString(text[:m[0]]) {
			continue
		}
		b.WriteString(text[last:m[0]])
		b.WriteString(Placeholder)
		last = m[1]
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// secretShaped is a long run that is not a path, an identifier or a model
// name: it holds a digit and 16 or more characters with no /, _ or - between
// them. A UUID, x86_64-linux-gnu or Qwen2.5-Coder-32B-Instruct stays, and so,
// by the same rule, does a base64url token whose - and _ fall less than 16
// characters apart, and a token of letters alone; a name before either
// still has it redacted.
func secretShaped(run string) bool {
	if !strings.ContainsAny(run, "0123456789") {
		return false
	}
	longest := 0
	for _, part := range strings.FieldsFunc(run, func(r rune) bool { return r == '/' || r == '_' || r == '-' }) {
		longest = max(longest, len(part))
	}
	return longest >= 16
}

// How far a value runs after its name.
type valueForm int

const (
	// valueWord runs to a space or a delimiter.
	valueWord valueForm = iota
	// valueRest runs to the end of the text: a header's whole value.
	valueRest
	// valuePhrase is a valueWord that is not a common word ("the key is
	// missing" names no key).
	valuePhrase
	// valueSchemeRest is a valueRest that starts with an auth scheme word,
	// and nothing otherwise ("Authorization header missing" names none).
	valueSchemeRest
)

// commonWords follow "key is" in messages that name no secret.
var commonWords = map[string]bool{
	"a": true, "an": true, "the": true, "not": true, "no": true, "missing": true, "invalid": true,
	"required": true, "empty": true, "expired": true, "wrong": true, "incorrect": true, "set": true,
	"unset": true, "none": true, "null": true, "too": true, "being": true, "now": true, "already": true,
}

// redactValues replaces the value after each match of name. A quoted value,
// with plain or escaped quotes, is redacted to its closing quote, spaces
// included; an unclosed quote to the end.
func redactValues(text string, name *regexp.Regexp, form valueForm) string {
	var b strings.Builder
	last := 0
	for _, m := range name.FindAllStringIndex(text, -1) {
		if m[0] < last {
			continue
		}
		start, end := valueSpan(text, m[1], form)
		value := text[start:end]
		if value == "" || strings.HasPrefix(text[start:], Placeholder) || form == valuePhrase && commonWords[strings.ToLower(strings.TrimRight(value, ".:!"))] {
			continue
		}
		// tokens: 4096, monkey: 5: a count after a name that only holds key
		// or token. Never after a credential's own name (strongWords).
		if form != valueRest && form != valueSchemeRest && shortCount(value) && weakName(text[m[0]:m[1]]) {
			continue
		}
		b.WriteString(text[last:start])
		b.WriteString(Placeholder)
		last = end
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// valueSpan is the value starting at i, without its quotes.
func valueSpan(text string, i int, form valueForm) (int, int) {
	rest := text[i:]
	switch {
	case strings.HasPrefix(rest, `\"`):
		if end := strings.Index(rest[2:], `\"`); end >= 0 {
			return i + 2, i + 2 + end
		}
		return i + 2, len(text)
	case strings.HasPrefix(rest, `"`):
		for j := 1; j < len(rest); j++ {
			switch rest[j] {
			case '\\':
				j++
			case '"':
				return i + 1, i + j
			}
		}
		return i + 1, len(text)
	case strings.HasPrefix(rest, `'`):
		if end := strings.IndexByte(rest[1:], '\''); end >= 0 {
			return i + 1, i + 1 + end
		}
		return i + 1, len(text)
	case form == valueRest:
		return i, len(text)
	case form == valueSchemeRest:
		if authSchemes[strings.ToLower(rest[:wordEnd(rest)])] {
			return i, len(text)
		}
		return i, i
	}
	end := wordEnd(rest)
	// "auth: Basic <credential>": the scheme and the credential after it.
	if authSchemes[strings.ToLower(rest[:end])] && strings.HasPrefix(rest[end:], " ") && end+1 < len(rest) {
		end += 1 + wordEnd(rest[end+1:])
	}
	return i, i + end
}

// wordEnd is where an unquoted value starting text ends: at a space or a
// delimiter, or at the end.
func wordEnd(text string) int {
	if end := strings.IndexAny(text, " \"',;&)]}<"); end >= 0 {
		return end
	}
	return len(text)
}
