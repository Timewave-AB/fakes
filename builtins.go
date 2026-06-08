package fakes

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// builtins is the registry of {name(args)} functions. Two kinds: derivations read
// the digits emitted so far in the current expansion (luhn, mod11, ean — place
// them after their payload); generators read only the rng (uuid, ulid, ...). All
// must stay pure over (rng, emitted, args) so a seeded faker is reproducible — a
// time-based id (uuid v7, ulid, objectid) draws its timestamp from the rng, not
// the wall clock. Add a builtin only for what data can't express: a random v4
// UUID already ships as data (data/misc/uuid.json), so the builtin is v7.
var builtins = map[string]builtin{
	"luhn":     {arity: 0, call: func(_ *session, e string, _ []string) string { return string(rune('0' + luhnCheck(e))) }},
	"mod11":    {arity: 0, call: func(_ *session, e string, _ []string) string { return mod11Check(e) }},
	"ean":      {arity: 0, call: func(_ *session, e string, _ []string) string { return eanCheck(e) }},
	"uuid":     {arity: 0, call: func(s *session, _ string, _ []string) string { return uuidV7(s) }},
	"ulid":     {arity: 0, call: func(s *session, _ string, _ []string) string { return ulid(s) }},
	"objectid": {arity: 0, call: func(s *session, _ string, _ []string) string { return randHex(s, 24) }},
	"nanoid":   {arity: 1, check: posIntArg, call: func(s *session, _ string, a []string) string { return nanoid(s, atoi(a[0])) }},
	"hex":      {arity: 1, check: posIntArg, call: func(s *session, _ string, a []string) string { return randHex(s, atoi(a[0])) }},
	"base64": {arity: 1, check: posIntArg, call: func(s *session, _ string, a []string) string {
		return base64.StdEncoding.EncodeToString(randBytes(s, atoi(a[0])))
	}},
	"int": {arity: 2, check: intRangeArgs, call: func(s *session, _ string, a []string) string {
		return strconv.Itoa(atoi(a[0]) + s.IntN(atoi(a[1])-atoi(a[0])+1))
	}},
	"float": {arity: 3, check: floatArgs, call: floatCall},
	"iban":  {arity: 1, check: ibanArg, call: func(s *session, _ string, a []string) string { return iban(s, a[0]) }},
	// seq is the one stateful builtin: a per-session counter from 1, advancing on
	// each call. An optional name selects an independent counter; no name uses the
	// default one. Deterministic by construction, so a seeded faker stays stable.
	"seq": {arity: -1, check: seqArg, call: func(s *session, _ string, a []string) string {
		key := ""
		if len(a) == 1 {
			key = a[0]
		}
		return strconv.FormatUint(s.next(key), 10)
	}},
}

const hexDigits = "0123456789abcdef"

// atoi parses an arg already validated by a builtin's check, so it cannot fail.
func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func randBytes(r rng, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.IntN(256))
	}
	return b
}

func randHex(r rng, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = hexDigits[r.IntN(16)]
	}
	return string(b)
}

func posIntArg(a []string) error {
	if n, err := strconv.Atoi(a[0]); err != nil || n < 1 {
		return fmt.Errorf("count %q must be a positive integer", a[0])
	}
	return nil
}

func intRangeArgs(a []string) error {
	lo, e1 := strconv.Atoi(a[0])
	hi, e2 := strconv.Atoi(a[1])
	if e1 != nil || e2 != nil {
		return fmt.Errorf("int(min,max) needs integer args, got %q,%q", a[0], a[1])
	}
	if lo > hi {
		return fmt.Errorf("int(min,max): min %d > max %d", lo, hi)
	}
	return nil
}

func floatArgs(a []string) error {
	lo, e1 := strconv.ParseFloat(a[0], 64)
	hi, e2 := strconv.ParseFloat(a[1], 64)
	dp, e3 := strconv.Atoi(a[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return fmt.Errorf("float(min,max,dp) needs numeric args, got %q,%q,%q", a[0], a[1], a[2])
	}
	if lo > hi {
		return fmt.Errorf("float(min,max,dp): min %v > max %v", lo, hi)
	}
	if dp < 0 {
		return fmt.Errorf("float(min,max,dp): decimals %d < 0", dp)
	}
	return nil
}

func floatCall(s *session, _ string, a []string) string {
	lo, _ := strconv.ParseFloat(a[0], 64)
	hi, _ := strconv.ParseFloat(a[1], 64)
	return strconv.FormatFloat(lo+s.Float64()*(hi-lo), 'f', atoi(a[2]), 64)
}

func seqArg(a []string) error {
	if len(a) > 1 {
		return fmt.Errorf("seq takes at most one name, got %d args", len(a))
	}
	if len(a) == 1 && a[0] == "" {
		return fmt.Errorf("seq name must not be empty")
	}
	return nil
}

// nanoidAlphabet is the 64-char URL-safe set Nano IDs use (order is irrelevant
// to the uniform pick).
const nanoidAlphabet = "_-0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func nanoid(r rng, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = nanoidAlphabet[r.IntN(len(nanoidAlphabet))]
	}
	return string(b)
}

// uuidV7 builds an RFC 9562 v7 UUID. The 48-bit timestamp field is drawn from
// the rng (not the clock) to stay reproducible, then the version (7) and variant
// (10) bits are forced; the rest is random.
func uuidV7(r rng) string {
	b := randBytes(r, 16)
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	var sb strings.Builder
	for i, x := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			sb.WriteByte('-')
		}
		sb.WriteByte(hexDigits[x>>4])
		sb.WriteByte(hexDigits[x&0x0f])
	}
	return sb.String()
}

// crockford is the ULID/Crockford base32 alphabet (no I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ulid builds a 26-char ULID: 128 random bits (timestamp from the rng) encoded
// big-endian into base32, the 130-bit stream left-padded with two zero bits.
func ulid(r rng) string {
	b := randBytes(r, 16)
	out := make([]byte, 26)
	for i := range out {
		v := 0
		for j := 0; j < 5; j++ { // 5 bits per char; the two leading pad bits are zero
			real := 5*i + j - 2
			bit := 0
			if real >= 0 {
				bit = int(b[real/8]>>(7-uint(real%8))) & 1
			}
			v = v<<1 | bit
		}
		out[i] = crockford[v]
	}
	return string(out)
}

// luhnCheck returns the Luhn check digit (0-9) over the digits of s; non-digit
// runes are skipped. Doubling runs from the rightmost digit, so the result is
// correct whatever the payload length.
func luhnCheck(s string) int {
	sum, double := 0, true
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		double = !double
		sum += d
	}
	return (10 - sum%10) % 10
}

// mod11Check returns the weighted mod-11 check character over the digits of s
// (weights 2..7 cycling from the right). A would-be value of 10 emits 'X', as in
// ISBN-10 / ISO 7064; non-digits are skipped.
func mod11Check(s string) string {
	sum, w := 0, 2
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		sum += int(c-'0') * w
		if w++; w > 7 {
			w = 2
		}
	}
	if chk := (11 - sum%11) % 11; chk != 10 {
		return string(rune('0' + chk))
	}
	return "X"
}

// eanCheck returns the EAN-13 / UPC-A / ISBN-13 / GTIN check digit over the
// digits of s: weights 3 and 1 alternating from the rightmost digit, mod 10.
func eanCheck(s string) string {
	sum, w := 0, 3
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		sum += int(c-'0') * w
		w = 4 - w // 3 <-> 1
	}
	return string(rune('0' + (10-sum%10)%10))
}

// ibanLen maps a supported country code to the full IBAN length. The check digits
// sit between the country code and the BBAN, so — unlike luhn/ean — iban can't be
// a left-to-right derivation; it generates the whole value instead.
var ibanLen = map[string]int{"BE": 16, "DE": 22, "DK": 18, "ES": 24, "FI": 18, "NO": 15, "SE": 24}

func ibanArg(a []string) error {
	if _, ok := ibanLen[a[0]]; !ok {
		return fmt.Errorf("iban(%q): unsupported country code", a[0])
	}
	return nil
}

// iban generates a structurally valid IBAN for cc: a numeric BBAN of the right
// length, then mod-97 check digits. Real bank/branch structure isn't modelled —
// the result passes length and checksum validation, which is what fake data needs.
func iban(r rng, cc string) string {
	bban := make([]byte, ibanLen[cc]-4)
	for i := range bban {
		bban[i] = byte('0' + r.IntN(10))
	}
	rem := 0
	feed := func(d int) { rem = (rem*10 + d) % 97 }
	for _, c := range bban {
		feed(int(c - '0'))
	}
	for i := 0; i < len(cc); i++ { // letters A-Z -> 10..35, fed as two digits
		v := int(cc[i]-'A') + 10
		feed(v / 10)
		feed(v % 10)
	}
	feed(0)
	feed(0)
	return fmt.Sprintf("%s%02d%s", cc, 98-rem, bban)
}
