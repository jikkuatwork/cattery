package normalize

import (
	"strconv"
	"strings"
)

const maxNumber = int64(999_999_999)

var ones = []string{
	"zero",
	"one",
	"two",
	"three",
	"four",
	"five",
	"six",
	"seven",
	"eight",
	"nine",
	"ten",
	"eleven",
	"twelve",
	"thirteen",
	"fourteen",
	"fifteen",
	"sixteen",
	"seventeen",
	"eighteen",
	"nineteen",
}

var tens = []string{
	"",
	"",
	"twenty",
	"thirty",
	"forty",
	"fifty",
	"sixty",
	"seventy",
	"eighty",
	"ninety",
}

var smallOrdinals = map[int64]string{
	0:  "zeroth",
	1:  "first",
	2:  "second",
	3:  "third",
	4:  "fourth",
	5:  "fifth",
	6:  "sixth",
	7:  "seventh",
	8:  "eighth",
	9:  "ninth",
	10: "tenth",
	11: "eleventh",
	12: "twelfth",
	13: "thirteenth",
	14: "fourteenth",
	15: "fifteenth",
	16: "sixteenth",
	17: "seventeenth",
	18: "eighteenth",
	19: "nineteenth",
}

var tensOrdinals = map[int64]string{
	20: "twentieth",
	30: "thirtieth",
	40: "fortieth",
	50: "fiftieth",
	60: "sixtieth",
	70: "seventieth",
	80: "eightieth",
	90: "ninetieth",
}

func cardinal(n int64) string {
	switch {
	case n < 0:
		return "minus " + cardinal(-n)
	case n < 20:
		return ones[n]
	case n < 100:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + " " + ones[n%10]
	case n < 1_000:
		head := ones[n/100] + " hundred"
		if n%100 == 0 {
			return head
		}
		return head + " and " + cardinal(n%100)
	case n < 1_000_000:
		head := cardinal(n/1_000) + " thousand"
		if n%1_000 == 0 {
			return head
		}
		return head + " " + cardinal(n%1_000)
	case n < 1_000_000_000:
		head := cardinal(n/1_000_000) + " million"
		if n%1_000_000 == 0 {
			return head
		}
		return head + " " + cardinal(n%1_000_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func ordinal(n int64) string {
	switch {
	case n < 0:
		return "minus " + ordinal(-n)
	case n < 20:
		return smallOrdinals[n]
	case n < 100:
		if n%10 == 0 {
			return tensOrdinals[n]
		}
		return tens[n/10] + " " + ordinal(n%10)
	case n < 1_000:
		head := ones[n/100] + " hundred"
		if n%100 == 0 {
			return head + "th"
		}
		return head + " and " + ordinal(n%100)
	case n < 1_000_000:
		head := cardinal(n/1_000) + " thousand"
		if n%1_000 == 0 {
			return head + "th"
		}
		return head + " " + ordinal(n%1_000)
	case n < 1_000_000_000:
		head := cardinal(n/1_000_000) + " million"
		if n%1_000_000 == 0 {
			return head + "th"
		}
		return head + " " + ordinal(n%1_000_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func decimal(s string) string {
	clean := strings.ReplaceAll(s, ",", "")
	parts := strings.SplitN(clean, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return s
	}

	whole, ok := parseNumber(parts[0])
	if !ok || whole > maxNumber {
		return s
	}

	frac := make([]string, 0, len(parts[1]))
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			return s
		}
		frac = append(frac, ones[r-'0'])
	}

	return cardinal(whole) + " point " + strings.Join(frac, " ")
}

func year(n int) string {
	switch {
	case n < 100:
		return cardinal(int64(n))
	case n == 2000:
		return "two thousand"
	case n > 2000 && n < 2010:
		return "two thousand " + cardinal(int64(n-2000))
	case n >= 2010 && n < 2100:
		return "twenty " + cardinal(int64(n-2000))
	default:
		head := n / 100
		tail := n % 100
		if tail == 0 {
			return cardinal(int64(head)) + " hundred"
		}
		if tail < 10 {
			return cardinal(int64(head)) + " oh " + cardinal(int64(tail))
		}
		return cardinal(int64(head)) + " " + cardinal(int64(tail))
	}
}

func minuteWords(n int) string {
	switch {
	case n <= 0:
		return ""
	case n < 10:
		return "oh " + ones[n]
	default:
		return cardinal(int64(n))
	}
}

func speakNumber(s string) string {
	if strings.ContainsRune(s, '.') {
		return decimal(s)
	}

	n, ok := parseNumber(s)
	if !ok || n > maxNumber {
		return ""
	}
	return cardinal(n)
}

func parseNumber(s string) (int64, bool) {
	clean := strings.ReplaceAll(s, ",", "")
	if clean == "" {
		return 0, false
	}

	n, err := strconv.ParseInt(clean, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseMinorUnits(s string) (int64, bool) {
	if s == "" {
		return 0, true
	}
	if len(s) == 1 {
		s += "0"
	}
	if len(s) != 2 {
		return 0, false
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
