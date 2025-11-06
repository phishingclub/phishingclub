package g

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/url"
	"strconv"
)

type (
	// A struct that wraps an String for encoding.
	encode struct{ str String }

	// A struct that wraps an String for decoding.
	decode struct{ str String }
)

// Encode returns an encode struct wrapping the given String.
func (s String) Encode() encode { return encode{s} }

// Decode returns a decode struct wrapping the given String.
func (s String) Decode() decode { return decode{s} }

// Base64 encodes the wrapped String using Base64 and returns the encoded result as an String.
func (e encode) Base64() String { return String(base64.StdEncoding.EncodeToString(e.str.Bytes())) }

// Base64 decodes the wrapped String using Base64 and returns the decoded result as Result[String].
func (d decode) Base64() Result[String] {
	decoded, err := base64.StdEncoding.DecodeString(d.str.Std())
	if err != nil {
		return Err[String](err)
	}

	return Ok(String(decoded))
}

// JSON encodes the provided string as JSON and returns the result as Result[String].
func (e encode) JSON() Result[String] {
	jsonData, err := json.Marshal(e.str)
	if err != nil {
		return Err[String](err)
	}

	return Ok(String(jsonData))
}

// JSON decodes the provided JSON string and returns the result as Result[String].
func (d decode) JSON() Result[String] {
	var data String
	err := json.Unmarshal(d.str.Bytes(), &data)
	if err != nil {
		return Err[String](err)
	}

	return Ok(data)
}

// URL encodes the input string, escaping reserved characters as per RFC 2396.
// If safe characters are provided, they will not be encoded.
//
// Parameters:
//
// - safe (String): Optional. Characters to exclude from encoding.
// If provided, the function will not encode these characters.
//
// Returns:
//
// - String: Encoded URL string.
func (e encode) URL(safe ...String) String {
	reserved := String(";/?:@&=+$,") // Reserved characters as per RFC 2396
	if len(safe) != 0 {
		reserved = safe[0]
	}

	var b Builder

	for _, r := range e.str {
		if reserved.ContainsRune(r) {
			b.WriteRune(r)
			continue
		}

		_, _ = b.WriteString(String(url.QueryEscape(string(r))))
	}

	return b.String()
}

// URL URL-decodes the wrapped String and returns the decoded result as Result[String].
func (d decode) URL() Result[String] {
	result, err := url.QueryUnescape(d.str.Std())
	if err != nil {
		return Err[String](err)
	}

	return Ok(String(result))
}

// HTML HTML-encodes the wrapped String and returns the encoded result as an String.
func (e encode) HTML() String { return String(html.EscapeString(e.str.Std())) }

// HTML HTML-decodes the wrapped String and returns the decoded result as an String.
func (d decode) HTML() String { return String(html.UnescapeString(d.str.Std())) }

// Rot13 encodes the wrapped String using ROT13 cipher and returns the encoded result as an
// String.
func (e encode) Rot13() String {
	rot := func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+13)%26
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+13)%26
		default:
			return r
		}
	}

	return e.str.Map(rot)
}

// Rot13 decodes the wrapped String using ROT13 cipher and returns the decoded result as an
// String.
func (d decode) Rot13() String { return d.str.Encode().Rot13() }

// XOR encodes the wrapped String using XOR cipher with the given key and returns the encoded
// result as an String.
func (e encode) XOR(key String) String {
	if key.Empty() {
		return e.str
	}

	encrypted := e.str.Bytes()

	for i := range len(e.str) {
		encrypted[i] ^= key[i%len(key)]
	}

	return String(encrypted)
}

// XOR decodes the wrapped String using XOR cipher with the given key and returns the decoded
// result as an String.
func (d decode) XOR(key String) String { return d.str.Encode().XOR(key) }

// Hex hex-encodes the wrapped String and returns the encoded result as an String.
func (e encode) Hex() String {
	var b Builder
	for i := range len(e.str) {
		b.WriteString(Int(e.str[i]).Hex())
	}

	return b.String()
}

// Hex hex-decodes the wrapped String and returns the decoded result as Result[String].
func (d decode) Hex() Result[String] {
	result, err := hex.DecodeString(d.str.Std())
	if err != nil {
		return Err[String](err)
	}

	return Ok(String(result))
}

// Octal returns the octal representation of the encoded string.
func (e encode) Octal() String {
	result := NewSlice[String](e.str.LenRunes())
	for i, char := range e.str.Runes() {
		result.Set(Int(i), Int(char).Octal())
	}

	return result.Join(" ")
}

// Octal returns the octal representation of the decimal-encoded string as Result[String].
func (d decode) Octal() Result[String] {
	var b Builder

	for v := range d.str.Split(" ") {
		n, err := strconv.ParseUint(v.Std(), 8, 32)
		if err != nil {
			return Err[String](err)
		}

		b.WriteRune(rune(n))
	}

	return Ok(b.String())
}

// Binary converts the wrapped String to its binary representation as an String.
func (e encode) Binary() String {
	var b Builder
	for i := range len(e.str) {
		b.WriteString(Int(e.str[i]).Binary())
	}

	return b.String()
}

// Binary converts the wrapped binary String back to its original String representation as Result[String].
func (d decode) Binary() Result[String] {
	var result Bytes

	for i := 0; i+8 <= len(d.str); i += 8 {
		b, err := strconv.ParseUint(d.str[i:i+8].Std(), 2, 8)
		if err != nil {
			return Err[String](err)
		}

		result = append(result, byte(b))
	}

	return Ok(result.String())
}
