package normalize

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	currencyRE          = compileCurrencyRE()
	titleRE             = compileLiteralRE(titles)
	unitRE              = compileLiteralRE(units)
	abbreviationRE      = compileLiteralRE(abbreviations)
	symbolRE            = compileSymbolRE()
	percentRE           = regexp.MustCompile(`\b(\d[\d,]*(?:\.\d+)?)\s*%`)
	decimalRE           = regexp.MustCompile(`\b(\d[\d,]*\.\d+)\b`)
	ordinalRE           = regexp.MustCompile(`\b(\d[\d,]*)(st|nd|rd|th)\b`)
	dateRE              = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{2,4})\b`)
	timeRE              = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*([AaPp][Mm])\b`)
	mixedScientificRE   = regexp.MustCompile(`\b(?:[a-z]+[A-Z]{2,}|[a-z][A-Z])\b`)
	possessiveAcronymRE = regexp.MustCompile(`\b([A-Z]{2,})'s\b`)
	acronymRE           = regexp.MustCompile(`\b([A-Z]{2,})\b`)
	cardinalTokenRE     = regexp.MustCompile(`\d[\d,]*`)
	multiSpaceRE        = regexp.MustCompile(`\s+`)
	spaceBeforePunctRE  = regexp.MustCompile(`\s+([,.;!?])`)
	currencySymbolRunes = currencyRuneSet()
)

// Normalize converts written English text to spoken form for TTS.
func Normalize(text string) string {
	out := strings.TrimSpace(text)
	if out == "" {
		return ""
	}

	out = replaceTitles(out)
	out = replaceCurrency(out)
	out = replacePercentages(out)
	out = replaceDecimals(out)
	out = replaceOrdinals(out)
	out = replaceCardinals(out)
	out = replaceDates(out)
	out = replaceTimes(out)
	out = replaceMixedScientific(out)
	out = replacePossessiveAcronyms(out)
	out = replaceAcronyms(out)
	out = replaceSymbols(out)
	out = replaceUnits(out)
	out = replaceAbbreviations(out)
	return cleanup(out)
}

func replaceTitles(text string) string {
	return replaceSubmatches(titleRE, text, func(src string, groups []string, span [2]int) string {
		if !hasTokenBoundaries(src, span[0], span[1]) {
			return groups[0]
		}
		return titles[strings.ToLower(groups[0])]
	})
}

func replaceCurrency(text string) string {
	return replaceSubmatches(currencyRE, text, func(_ string, groups []string, _ [2]int) string {
		info, ok := currencies[groups[1]]
		if !ok {
			return groups[0]
		}

		major, ok := parseNumber(groups[2])
		if !ok || major > maxNumber {
			return groups[0]
		}

		minor, ok := parseMinorUnits(groups[3])
		if !ok {
			return groups[0]
		}

		return formatCurrency(info, major, minor)
	})
}

func replacePercentages(text string) string {
	return replaceSubmatches(percentRE, text, func(_ string, groups []string, _ [2]int) string {
		spoken := speakNumber(groups[1])
		if spoken == "" {
			return groups[0]
		}
		return spoken + " percent"
	})
}

func replaceDecimals(text string) string {
	return replaceSubmatches(decimalRE, text, func(src string, groups []string, span [2]int) string {
		if isJoinedNumber(src, span[0], span[1]) {
			return groups[0]
		}

		spoken := decimal(groups[1])
		if spoken == groups[1] {
			return groups[0]
		}
		return spoken
	})
}

func replaceOrdinals(text string) string {
	return replaceSubmatches(ordinalRE, text, func(_ string, groups []string, _ [2]int) string {
		n, ok := parseNumber(groups[1])
		if !ok || n > maxNumber {
			return groups[0]
		}
		return ordinal(n)
	})
}

func replaceCardinals(text string) string {
	matches := cardinalTokenRE.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var b strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		b.WriteString(text[last:start])

		token := text[start:end]
		if shouldExpandCardinal(text, start, end) {
			if n, ok := parseNumber(token); ok && n <= maxNumber {
				token = cardinal(n)
			}
		}

		b.WriteString(token)
		last = end
	}

	b.WriteString(text[last:])
	return b.String()
}

func replaceDates(text string) string {
	return replaceSubmatches(dateRE, text, func(_ string, groups []string, _ [2]int) string {
		month, err := strconv.Atoi(groups[1])
		if err != nil || month < 1 || month >= len(months) {
			return groups[0]
		}

		day, err := strconv.Atoi(groups[2])
		if err != nil || day < 1 || day > 31 {
			return groups[0]
		}

		yearNum, err := strconv.Atoi(groups[3])
		if err != nil {
			return groups[0]
		}

		yearWords := cardinal(int64(yearNum))
		if len(groups[3]) == 4 {
			yearWords = year(yearNum)
		}

		return months[month] + " " + ordinal(int64(day)) + ", " + yearWords
	})
}

func replaceTimes(text string) string {
	return replaceSubmatches(timeRE, text, func(_ string, groups []string, _ [2]int) string {
		hour, err := strconv.Atoi(groups[1])
		if err != nil || hour < 1 || hour > 12 {
			return groups[0]
		}

		minutes := 0
		if groups[2] != "" {
			minutes, err = strconv.Atoi(groups[2])
			if err != nil || minutes < 0 || minutes > 59 {
				return groups[0]
			}
		}

		parts := []string{cardinal(int64(hour))}
		if minutes > 0 {
			parts = append(parts, minuteWords(minutes))
		}

		if strings.HasPrefix(strings.ToLower(groups[3]), "a") {
			parts = append(parts, "a m")
		} else {
			parts = append(parts, "p m")
		}

		return strings.Join(parts, " ")
	})
}

func replaceMixedScientific(text string) string {
	return mixedScientificRE.ReplaceAllStringFunc(text, spellOut)
}

func replacePossessiveAcronyms(text string) string {
	return replaceSubmatches(possessiveAcronymRE, text, func(_ string, groups []string, _ [2]int) string {
		if speaksAsWord(groups[1]) {
			return groups[0]
		}
		return spellOut(groups[1]) + "'s"
	})
}

func replaceAcronyms(text string) string {
	return replaceSubmatches(acronymRE, text, func(_ string, groups []string, _ [2]int) string {
		if speaksAsWord(groups[1]) {
			return groups[1]
		}
		return spellOut(groups[1])
	})
}

func replaceSymbols(text string) string {
	return symbolRE.ReplaceAllStringFunc(text, func(match string) string {
		return " " + symbols[match] + " "
	})
}

func replaceUnits(text string) string {
	return replaceSubmatches(unitRE, text, func(src string, groups []string, span [2]int) string {
		if !hasTokenBoundaries(src, span[0], span[1]) {
			return groups[0]
		}
		return units[strings.ToLower(groups[0])]
	})
}

func replaceAbbreviations(text string) string {
	return replaceSubmatches(abbreviationRE, text, func(src string, groups []string, span [2]int) string {
		if !hasTokenBoundaries(src, span[0], span[1]) {
			return groups[0]
		}
		return abbreviations[strings.ToLower(groups[0])]
	})
}

func formatCurrency(info currencyNames, major, minor int64) string {
	parts := make([]string, 0, 3)

	if major > 0 || minor == 0 {
		name := info.majorPlural
		if major == 1 {
			name = info.majorSingular
		}
		parts = append(parts, cardinal(major)+" "+name)
	}

	if minor > 0 {
		name := info.minorPlural
		if minor == 1 {
			name = info.minorSingular
		}
		phrase := cardinal(minor) + " " + name
		if len(parts) > 0 {
			parts = append(parts, "and "+phrase)
		} else {
			parts = append(parts, phrase)
		}
	}

	return strings.Join(parts, " ")
}

func shouldExpandCardinal(text string, start, end int) bool {
	prev, prevSize := lastRune(text[:start])
	next, nextSize := firstRune(text[end:])

	if unicode.IsLetter(prev) || unicode.IsLetter(next) {
		return false
	}

	if prev == '/' || next == '/' || prev == ':' || next == ':' ||
		prev == '%' || next == '%' || prev == '\'' || next == '\'' {
		return false
	}

	if _, ok := currencySymbolRunes[prev]; ok {
		return false
	}
	if _, ok := currencySymbolRunes[next]; ok {
		return false
	}

	if next == '.' {
		afterDot, _ := firstRune(text[end+nextSize:])
		if unicode.IsDigit(afterDot) {
			return false
		}
	}

	if prev == '.' {
		beforeDot, _ := lastRune(text[:start-prevSize])
		if unicode.IsDigit(beforeDot) {
			return false
		}
	}

	return true
}

func isJoinedNumber(text string, start, end int) bool {
	prev, _ := lastRune(text[:start])
	next, nextSize := firstRune(text[end:])

	if next == '.' {
		afterDot, _ := firstRune(text[end+nextSize:])
		if unicode.IsDigit(afterDot) {
			return true
		}
	}

	if prev == '.' {
		beforeDot, _ := lastRune(text[:start-1])
		if unicode.IsDigit(beforeDot) {
			return true
		}
	}

	return false
}

func spellOut(token string) string {
	letters := make([]string, 0, utf8.RuneCountInString(token))
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters = append(letters, strings.ToUpper(string(r)))
		}
	}
	return strings.Join(letters, " ")
}

func speaksAsWord(token string) bool {
	_, ok := spokenAsWord[token]
	return ok
}

func cleanup(text string) string {
	text = multiSpaceRE.ReplaceAllString(text, " ")
	text = spaceBeforePunctRE.ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}

func hasTokenBoundaries(text string, start, end int) bool {
	prev, _ := lastRune(text[:start])
	next, _ := firstRune(text[end:])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev) &&
		!unicode.IsLetter(next) && !unicode.IsDigit(next)
}

func replaceSubmatches(
	re *regexp.Regexp,
	src string,
	fn func(src string, groups []string, span [2]int) string,
) string {
	matches := re.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return src
	}

	var b strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		if start < last {
			continue
		}

		b.WriteString(src[last:start])

		groups := make([]string, len(match)/2)
		for i := 0; i < len(match); i += 2 {
			if match[i] == -1 || match[i+1] == -1 {
				continue
			}
			groups[i/2] = src[match[i]:match[i+1]]
		}

		b.WriteString(fn(src, groups, [2]int{start, end}))
		last = end
	}

	b.WriteString(src[last:])
	return b.String()
}

func compileLiteralRE(dict map[string]string) *regexp.Regexp {
	keys := sortedKeys(dict)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, regexp.QuoteMeta(key))
	}
	return regexp.MustCompile(`(?i)(?:` + strings.Join(parts, `|`) + `)`)
}

func compileCurrencyRE() *regexp.Regexp {
	keys := sortedKeys(currencies)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, regexp.QuoteMeta(key))
	}
	return regexp.MustCompile(`(` + strings.Join(parts, `|`) + `)\s*(\d[\d,]*)(?:\.(\d{1,2}))?`)
}

func compileSymbolRE() *regexp.Regexp {
	keys := sortedKeys(symbols)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, regexp.QuoteMeta(key))
	}
	return regexp.MustCompile(strings.Join(parts, `|`))
}

func currencyRuneSet() map[rune]struct{} {
	out := make(map[rune]struct{}, len(currencies))
	for key := range currencies {
		r, size := utf8.DecodeRuneInString(key)
		if r != utf8.RuneError && size == len(key) {
			out[r] = struct{}{}
		}
	}
	return out
}

func sortedKeys[V any](dict map[string]V) []string {
	keys := make([]string, 0, len(dict))
	for key := range dict {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})

	return keys
}

func firstRune(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	return utf8.DecodeRuneInString(s)
}

func lastRune(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	return utf8.DecodeLastRuneInString(s)
}
