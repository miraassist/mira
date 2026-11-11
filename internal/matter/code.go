package matter

import (
	"errors"
	"fmt"
	"strings"
)

// A Matter setup code comes in two shapes (docs/spec/device-onboarding.md,
// Matter over Wi-Fi): the QR code's content, which begins with "MT:", and
// the manual pairing code of eleven digits (twenty-one for a device that
// carries its vendor and product in it). The last digit of the manual code
// is a check digit after Verhoeff, as Matter's specification prescribes, so
// a digit misheard at a satellite is caught before anything is sent.

// ErrCode says a code is none.
var ErrCode = errors.New("no matter setup code")

// Code is the setup code as Home Assistant wants it: the QR content as it
// is, a manual code as its digits. It errs when the code is none.
func Code(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if upper := strings.ToUpper(code); strings.HasPrefix(upper, "MT:") {
		if len(upper) <= len("MT:") {
			return "", fmt.Errorf("%w: the qr code holds nothing", ErrCode)
		}
		return upper, nil
	}
	digits := digitsOf(code)
	if len(digits) != 11 && len(digits) != 21 {
		return "", fmt.Errorf("%w: %d digits, not 11 or 21", ErrCode, len(digits))
	}
	if !verhoeff(digits) {
		return "", fmt.Errorf("%w: the check digit does not fit", ErrCode)
	}
	return digits, nil
}

// digitsOf keeps the digits of a text: a code read out comes with spaces,
// dashes or dots between its groups.
func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Verhoeff's tables: multiplication in the dihedral group D5, the
// permutation applied per position, and the inverses.
var (
	verhoeffD = [10][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
		{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
		{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
		{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
		{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
		{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
		{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
		{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
		{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
	}
	verhoeffP = [8][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
		{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
		{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
		{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
		{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
		{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
		{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
	}
)

// verhoeff reports whether a number including its check digit checks out.
func verhoeff(digits string) bool {
	c := 0
	for i, n := 0, len(digits); i < n; i++ {
		d := int(digits[n-i-1] - '0')
		c = verhoeffD[c][verhoeffP[i%8][d]]
	}
	return c == 0
}
