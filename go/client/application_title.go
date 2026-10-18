package client

import (
	"strings"
	"unicode"
)

// ReservedApplicationTitle reports a title an application may not register
// under: one that, folded, starts with "openabstractions", so a registered app
// never reads as OpenAbstractions or as a part of it ("OpenAbstractions
// Helper"). The applications service refuses such a title at Register,
// whether an operator or the app itself registers it; an entry stored before
// this check still loads, and a reader shows it under another name.
func ReservedApplicationTitle(title string) bool {
	return strings.HasPrefix(foldTitle(title), "openabstractions")
}

// titleConfusables are characters that read as the Latin letters of
// "openabstractions": letters of other scripts and small capitals, which
// NFKC keeps apart; modifier (superscript) letters, which NFKC maps; and the
// digit zero read as o.
var titleConfusables = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'н': 'n', 'о': 'o', 'р': 'p', 'с': 'c', 'т': 't', 'і': 'i', 'ѕ': 's', 'ј': 'j',
	'α': 'a', 'β': 'b', 'ε': 'e', 'η': 'n', 'ι': 'i', 'ο': 'o', 'ρ': 'p', 'τ': 't', 'ν': 'v',
	'ᴀ': 'a', 'ʙ': 'b', 'ᴄ': 'c', 'ᴇ': 'e', 'ɪ': 'i', 'ɴ': 'n', 'ᴏ': 'o', 'ᴘ': 'p', 'ʀ': 'r', 'ꜱ': 's', 'ᴛ': 't',
	'ᵃ': 'a', 'ᵇ': 'b', 'ᶜ': 'c', 'ᵉ': 'e', 'ⁱ': 'i', 'ⁿ': 'n', 'ᵒ': 'o', 'ᵖ': 'p', 'ʳ': 'r', 'ˢ': 's', 'ᵗ': 't',
	'0': 'o',
}

// foldTitle is the form ReservedApplicationTitle compares: the compatibility
// normalisation (NFKC) of the look-alikes a title can use for these letters
// (full-width forms, mathematical alphanumeric letters and digits, circled
// letters), the
// confusable letters above, letters and digits only (spaces, punctuation,
// zero-width and other format characters and combining marks dropped), and
// case folded. golang.org/x/text is not in this module, so the NFKC part
// covers those ranges rather than every compatibility mapping.
func foldTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		case r >= 0x1D400 && r <= 0x1D6A3:
			if k := (r - 0x1D400) % 52; k < 26 {
				r = 'A' + k
			} else {
				r = 'a' + k - 26
			}
		case r >= 0x1D7CE && r <= 0x1D7FF:
			r = '0' + (r-0x1D7CE)%10
		case r >= 0x24B6 && r <= 0x24CF:
			r = 'a' + r - 0x24B6
		case r >= 0x24D0 && r <= 0x24E9:
			r = 'a' + r - 0x24D0
		}
		r = unicode.ToLower(unicode.ToUpper(r))
		if mapped, ok := titleConfusables[r]; ok {
			r = mapped
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
