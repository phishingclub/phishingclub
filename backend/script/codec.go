package script

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"html"
	"io"
	"net/url"
	"strings"

	"github.com/dop251/goja"
	"github.com/google/uuid"
)

// registerCodec installs the encode/decode/hash/hmac/jwt/random helpers on the
// VM. These are the data-transform toolkit for scripts: base and binary
// encodings, hashing and keyed HMAC signing, JWT inspection, and secure random
// (nonces, PKCE, OAuth state). Output encoding for hash/hmac/random is selectable
// as "hex" (default), "base64", "base64url" or "base32".
func registerCodec(vm *goja.Runtime) {
	vm.Set("encode", map[string]interface{}{
		// text and url
		"base64":    func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		"base64url": func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) },
		"base32":    func(s string) string { return base32.StdEncoding.EncodeToString([]byte(s)) },
		"hex":       func(s string) string { return hex.EncodeToString([]byte(s)) },
		"url":       func(s string) string { return url.QueryEscape(s) },
		"urlPath":   func(s string) string { return url.PathEscape(s) },
		"html":      func(s string) string { return html.EscapeString(s) },
		"json": func(call goja.FunctionCall) goja.Value {
			b, err := json.Marshal(call.Argument(0).Export())
			if err != nil {
				panic(vm.NewGoError(err))
			}
			return vm.ToValue(string(b))
		},
		// object -> application/x-www-form-urlencoded
		"form": func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(formEncode(call.Argument(0)))
		},
		// compression, output as base64 so it survives as a JS string
		"gzip": func(s string) (string, error) { return deflateEncode(s, true) },
		"deflate": func(s string) (string, error) {
			return deflateEncode(s, false)
		},
	})

	vm.Set("decode", map[string]interface{}{
		"base64": func(s string) (string, error) {
			b, err := base64.StdEncoding.DecodeString(s)
			return string(b), err
		},
		"base64url": func(s string) (string, error) {
			b, err := decodeBase64URL(s)
			return string(b), err
		},
		"base32": func(s string) (string, error) {
			b, err := base32.StdEncoding.DecodeString(s)
			return string(b), err
		},
		"hex": func(s string) (string, error) {
			b, err := hex.DecodeString(s)
			return string(b), err
		},
		"url":     func(s string) (string, error) { return url.QueryUnescape(s) },
		"urlPath": func(s string) (string, error) { return url.PathUnescape(s) },
		"html":    func(s string) string { return html.UnescapeString(s) },
		"json": func(s string) (interface{}, error) {
			var v interface{}
			err := json.Unmarshal([]byte(s), &v)
			return v, err
		},
		// application/x-www-form-urlencoded -> object
		"form": func(s string) (map[string]interface{}, error) { return formDecode(s) },
		"gzip": func(s string) (string, error) { return inflateDecode(s, true) },
		"deflate": func(s string) (string, error) {
			return inflateDecode(s, false)
		},
	})

	// hashing: hash.sha256(input, enc?) -> string
	vm.Set("hash", map[string]interface{}{
		"md5":    hashFn(vm, func() hash.Hash { return md5.New() }),
		"sha1":   hashFn(vm, func() hash.Hash { return sha1.New() }),
		"sha256": hashFn(vm, func() hash.Hash { return sha256.New() }),
		"sha384": hashFn(vm, func() hash.Hash { return sha512.New384() }),
		"sha512": hashFn(vm, func() hash.Hash { return sha512.New() }),
	})

	// keyed HMAC: hmac.sha256(key, message, enc?) -> string
	vm.Set("hmac", map[string]interface{}{
		"sha1":   hmacFn(vm, func() hash.Hash { return sha1.New() }),
		"sha256": hmacFn(vm, func() hash.Hash { return sha256.New() }),
		"sha384": hmacFn(vm, func() hash.Hash { return sha512.New384() }),
		"sha512": hmacFn(vm, func() hash.Hash { return sha512.New() }),
	})

	// jwt.decode(token) -> { header, payload, signature } (NOT verified)
	vm.Set("jwt", map[string]interface{}{
		"decode": func(token string) (map[string]interface{}, error) { return jwtDecode(token) },
	})

	// random: nonces, PKCE verifiers, OAuth state
	vm.Set("random", map[string]interface{}{
		"bytes": func(call goja.FunctionCall) goja.Value {
			n := int(call.Argument(0).ToInteger())
			if n <= 0 || n > 4096 {
				panic(vm.NewTypeError("random.bytes: length must be 1..4096"))
			}
			b := make([]byte, n)
			if _, err := rand.Read(b); err != nil {
				panic(vm.NewGoError(err))
			}
			return vm.ToValue(codecOutput(b, argString(call, 1, "hex")))
		},
		"uuid": func() string { return uuid.NewString() },
	})
}

// codecOutput renders bytes in the requested encoding, defaulting to hex.
func codecOutput(b []byte, enc string) string {
	switch strings.ToLower(enc) {
	case "base64":
		return base64.StdEncoding.EncodeToString(b)
	case "base64url":
		return base64.RawURLEncoding.EncodeToString(b)
	case "base32":
		return base32.StdEncoding.EncodeToString(b)
	default:
		return hex.EncodeToString(b)
	}
}

// argString reads an optional string argument, falling back to def.
func argString(call goja.FunctionCall, i int, def string) string {
	if len(call.Arguments) > i {
		v := call.Argument(i)
		if !goja.IsUndefined(v) && !goja.IsNull(v) {
			return v.String()
		}
	}
	return def
}

func hashFn(vm *goja.Runtime, newH func() hash.Hash) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		h := newH()
		h.Write([]byte(call.Argument(0).String()))
		return vm.ToValue(codecOutput(h.Sum(nil), argString(call, 1, "hex")))
	}
}

func hmacFn(vm *goja.Runtime, newH func() hash.Hash) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		m := hmac.New(newH, []byte(call.Argument(0).String()))
		m.Write([]byte(call.Argument(1).String()))
		return vm.ToValue(codecOutput(m.Sum(nil), argString(call, 2, "hex")))
	}
}

// decodeBase64URL decodes url-safe base64 with or without padding.
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

func deflateEncode(s string, gz bool) (string, error) {
	var buf bytes.Buffer
	var w io.WriteCloser
	var err error
	if gz {
		w = gzip.NewWriter(&buf)
	} else {
		w, err = flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return "", err
		}
	}
	if _, err := w.Write([]byte(s)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func inflateDecode(b64 string, gz bool) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	var r io.ReadCloser
	if gz {
		r, err = gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
	} else {
		r = flate.NewReader(bytes.NewReader(data))
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// formEncode turns an object into an application/x-www-form-urlencoded string.
func formEncode(val goja.Value) string {
	values := url.Values{}
	if m, ok := val.Export().(map[string]interface{}); ok {
		for k, v := range m {
			switch vv := v.(type) {
			case []interface{}:
				for _, item := range vv {
					values.Add(k, fmt.Sprint(item))
				}
			default:
				values.Set(k, fmt.Sprint(v))
			}
		}
	}
	return values.Encode()
}

// formDecode parses a form-encoded string into an object. A key with one value
// becomes a string, a repeated key becomes an array.
func formDecode(s string) (map[string]interface{}, error) {
	vals, err := url.ParseQuery(s)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	for k, v := range vals {
		if len(v) == 1 {
			out[k] = v[0]
			continue
		}
		arr := make([]interface{}, len(v))
		for i, x := range v {
			arr[i] = x
		}
		out[k] = arr
	}
	return out, nil
}

// jwtDecode splits a JWT and returns its header and payload as objects plus the
// raw signature. It does NOT verify the signature: it is for inspecting tokens.
func jwtDecode(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid jwt: expected header.payload.signature")
	}
	header, err := decodeB64URLJSON(parts[0])
	if err != nil {
		return nil, fmt.Errorf("jwt header: %w", err)
	}
	payload, err := decodeB64URLJSON(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwt payload: %w", err)
	}
	sig := ""
	if len(parts) >= 3 {
		sig = parts[2]
	}
	return map[string]interface{}{
		"header":    header,
		"payload":   payload,
		"signature": sig,
	}, nil
}

func decodeB64URLJSON(s string) (interface{}, error) {
	b, err := decodeBase64URL(s)
	if err != nil {
		return nil, err
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}
