package places

import (
	"strings"
	"unicode"
)

// fold lowercases s and turns Latin letters with diacritics into plain
// ASCII ("København" -> "kobenhavn", "Düsseldorf" -> "dusseldorf"), so
// that searches match however the name is typed. Punctuation becomes a
// space; other scripts are kept lowercased.
func fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range s {
		if r < 128 {
			switch {
			case r >= 'A' && r <= 'Z':
				b.WriteRune(r + 32)
				space = false
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				b.WriteRune(r)
				space = false
			default:
				if !space {
					b.WriteByte(' ')
					space = true
				}
			}
			continue
		}
		if t, ok := latin[unicode.ToLower(r)]; ok {
			b.WriteString(t)
			space = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// Fold is fold for other packages (the gazetteer builder).
func Fold(s string) string { return fold(s) }

var latin = func() map[rune]string {
	m := map[rune]string{}
	add := func(to string, from string) {
		for _, r := range from {
			m[r] = to
		}
	}
	add("a", "àáâãäåāăąǎǟǡǻȁȃȧ")
	add("ae", "æǣǽ")
	add("c", "çćĉċč")
	add("d", "ďđð")
	add("e", "èéêëēĕėęěȅȇȩ")
	add("g", "ĝğġģǧǵ")
	add("h", "ĥħȟ")
	add("i", "ìíîïĩīĭįıǐȉȋ")
	add("j", "ĵ")
	add("k", "ķǩ")
	add("l", "ĺļľŀł")
	add("n", "ñńņňŉŋǹ")
	add("o", "òóôõöøōŏőǒǫǭǿȍȏȫȭȯȱ")
	add("oe", "œ")
	add("r", "ŕŗřȑȓ")
	add("s", "śŝşšșſ")
	add("ss", "ß")
	add("t", "ţťŧț")
	add("th", "þ")
	add("u", "ùúûüũūŭůűųǔǖǘǚǜȕȗ")
	add("w", "ŵ")
	add("y", "ýÿŷȳ")
	add("z", "źżžƶ")
	return m
}()
