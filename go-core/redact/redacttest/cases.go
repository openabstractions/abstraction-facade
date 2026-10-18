// Package redacttest is the bypass table redact.Line answers. The owner's
// test and every consumer's test run the same rows, so a consumer that stops
// passing provider text through redact fails where it shows the text.
package redacttest

// Case is one provider line and the text OA may show for it.
type Case struct {
	In, Want string
}

// Secrets are the credential values the table's inputs carry. None may
// appear in any Want. EXAMPLE marks the prefixed test keys as synthetic for
// the source-split credential scan without exempting this file from the scan.
var Secrets = []string{
	"abc.DEF-1", "AbCdEf0123456789EXAMPLE", "proj-abc_123", "abc123", "hunter2", "s3cr3t",
	"tabTok3n", "nbspTok3n", "quotedTok3n", "dXNlcjpwYXNzd29yZA==", "ghp_0123456789abcdefghijABCDEFGHIJEXAMPLE",
	"escTok3n", "after space", "flagTok3n", "phraseTok3n", "pa55word",
	"eyJhbGciOiJIUzI1NiJ9", "AKIAIOSFODNN7EXAMPLE",
	"Zm9yIGEgbG9uZyB0aW1lIEkgd2VudCB0byBiZWQgZWFybHk0MjQy",
	"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	"cfTok3n", "\u2066", "\u200c", "\u202e", "\u200b", "fusedTok3n", "escapeTok3n",
	"colonTok3n", "xmlTok3n", "dXNlcjpwYXNz", "pwdTok3n", "ppTok3n", "signTok3n",
	"AbC%2BdEf0123456789xyzQrStUvWx%3D", "X%2Babcdefghij0123456789ABCDEFGH%3D",
	"hunter2xyz", "passTok3n", "pwTok3n", "flagPassTok3n", "bareBasicTok3n", "98765432109",
}

// Cases is the bypass table: each input reached a provider's exit reason
// unredacted before redact owned the rule, or keeps text that must stay.
var Cases = []Case{
	// Token shapes the first safeTail already redacted.
	{"auth: bearer abc.DEF-1", "auth: [redacted]"},
	{"using hf_AbCdEf0123456789EXAMPLE for the pull", "using hf_[redacted] for the pull"},
	{"OPENAI sk-proj-abc_123 refused", "OPENAI sk-[redacted] refused"},
	{`{"api_key": "abc123", "model": "x"}`, `{"api_key": "[redacted]", "model": "x"}`},
	{"token=abc123&next=1", "token=[redacted]&next=1"},
	{"PASSWORD: hunter2", "PASSWORD: [redacted]"},
	{"client_secret='s3cr3t'", "client_secret='[redacted]'"},

	// A control or a Unicode space separates; it never joins the name to
	// the value.
	{"Bearer\ttabTok3n failed", "Bearer [redacted] failed"},
	{"Bearer\u00a0nbspTok3n failed", "Bearer [redacted] failed"},
	{`Bearer "quotedTok3n" failed`, `Bearer "[redacted]" failed`},
	{"Bearer\x1b[0mescapeTok3n", "Bearer [redacted]"},
	{"Bearer\u200bfusedTok3n", "Bearer[redacted]"},

	// An Authorization header's whole value goes, whatever its scheme.
	{"Authorization: Basic dXNlcjpwYXNzd29yZA==", "Authorization: [redacted]"},
	{"Authorization: token ghp_0123456789abcdefghijABCDEFGHIJEXAMPLE", "Authorization: [redacted]"},
	{`-H "Authorization: Bearer abc.DEF-1" sent`, `-H "Authorization: [redacted]`},
	{`{"Authorization": "Bearer abc.DEF-1", "model": "x"}`, `{"Authorization": "[redacted]", "model": "x"}`},
	{"Cookie: session=s3cr3t; theme=dark", "Cookie: [redacted]"},
	{"bearer: abc.DEF-1 refused", "bearer: [redacted] refused"},

	// Escaped quotes, a quoted value with a space, a flag and a phrase.
	{`{\"token\":\"escTok3n\"}`, `{\"token\":\"[redacted]\"}`},
	{`"token": "a after space", "model": "x"`, `"token": "[redacted]", "model": "x"`},
	{"run --token flagTok3n --model x", "run --token [redacted] --model x"},
	{"run --api-key=flagTok3n", "run --api-key=[redacted]"},
	{"the password is phraseTok3n", "the password is [redacted]"},
	{"the API key is missing", "the API key is missing"},

	// URL credentials, a JWT, an AWS key id, a long base64 blob and a hex
	// secret.
	{"GET https://user:pa55word@example.com/v1 failed", "GET https://[redacted]@example.com/v1 failed"},
	{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U ok", "jwt [redacted] ok"},
	{"aws AKIAIOSFODNN7EXAMPLE denied", "aws [redacted] denied"},
	{"blob Zm9yIGEgbG9uZyB0aW1lIEkgd2VudCB0byBiZWQgZWFybHk0MjQy end", "blob [redacted] end"},
	{"digest e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "digest [redacted]"},

	// Format characters go: a bidi override and zero-width characters.
	{"ok \u202edenied\u202c done", "ok denied done"},
	{"zw\u200b\u200c\u2060Tok3n", "zwTok3n"},
	{"bidi\u2066Tok3n\u2069 and hf_\u200bcfTok3n", "bidiTok3n and hf_[redacted]"},

	// Review of 2026-10-02: ":" after is, an XML element, auth, pwd,
	// passphrase, sig and signature names, and percent-encoded runs.
	{"the password is: colonTok3n", "the password is: [redacted]"},
	{"<password>xmlTok3n</password>", "<password>[redacted]</password>"},
	{"x-auth: Basic dXNlcjpwYXNz retry", "x-auth: [redacted] retry"},
	{"db_pwd=pwdTok3n next", "db_pwd=[redacted] next"},
	{"passphrase: ppTok3n", "passphrase: [redacted]"},
	{"signature=signTok3n", "signature=[redacted]"},
	{"GET https://acct.blob.core.windows.net/c/b?sv=2022&sig=AbC%2BdEf0123456789xyzQrStUvWx%3D&se=1", "GET https://acct.blob.core.windows.net/c/b?sv=2022&sig=[redacted]&se=1"},
	{"blob X%2Babcdefghij0123456789ABCDEFGH%3D end", "blob [redacted] end"},
	{"exited:signal: killed", "exited:signal: killed"},
	{"the author: Jane", "the author: Jane"},

	// Review of a67123429: => as one separator, pass and pw names, a bare
	// Authorization scheme, and what stays readable on purpose.
	{"password => hunter2xyz", "password => [redacted]"},
	{":password=>'hunter2xyz'", ":password=>'[redacted]'"},
	{"db_pass: passTok3n", "db_pass: [redacted]"},
	{"pw=pwTok3n", "pw=[redacted]"},
	{"run --pass flagPassTok3n now", "run --pass [redacted] now"},
	{"Authorization Basic bareBasicTok3n refused", "Authorization [redacted]"},
	{"Authorization header missing", "Authorization header missing"},
	{"tests passed: 12", "tests passed: 12"},
	{"bypass: 3", "bypass: 3"},
	{"tokens: 4096", "tokens: 4096"},
	{"max_tokens=512 monkey: 5", "max_tokens=512 monkey: 5"},
	// A count passes after key or token alone, never after a credential's
	// own name, and a long number is no count.
	{"pass: 1234", "pass: [redacted]"},
	{"api_key: 98765432109", "api_key: [redacted]"},
	// A word after key is redacted: a key's value can be a word.
	{"primary key: id", "primary key: [redacted]"},
	// A content digest names bytes; a bare 64-hex run is redacted.
	{"want sha256:50a031ca506d6dc6ce3cf0afd16716fa366f842a3eb13c35c5a170dcac5eb960 got other",
		"want sha256:50a031ca506d6dc6ce3cf0afd16716fa366f842a3eb13c35c5a170dcac5eb960 got other"},

	// What the long-token rule lets through by design, with no name before
	// it: a base64url token whose - and _ fall less than 16 characters
	// apart, and a token of letters alone.
	{"tok AbCdEfGh-IjKlMnOp_QrStUvWx-YzAbCdEf12 end", "tok AbCdEfGh-IjKlMnOp_QrStUvWx-YzAbCdEf12 end"},
	{"tok AbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIj end", "tok AbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIj end"},

	// Escapes and controls become spaces; text that is not a secret stays.
	{"\x1b[31merror\x1b[0m: model failed\a", "error : model failed"},
	{"\x1b]0;title\x07loading", "loading"},
	{"load /usr/lib/x86_64-linux-gnu/libvulkan.so.1 and Qwen2.5-Coder-32B-Instruct-Q4_K_M failed", "load /usr/lib/x86_64-linux-gnu/libvulkan.so.1 and Qwen2.5-Coder-32B-Instruct-Q4_K_M failed"},
	{"job 123e4567-e89b-12d3-a456-426614174000 ended", "job 123e4567-e89b-12d3-a456-426614174000 ended"},
	{"  surrounded  \r\n", "surrounded"},
}
