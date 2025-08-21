package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
)

func Encrypt(s string, secret string) (string, error) {
	block, err := aes.NewCipher([]byte(secret))
	if err != nil {
		return "", errs.Wrap(err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errs.Wrap(err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errs.Wrap(err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(s), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(s string, secret string) (string, error) {
	block, err := aes.NewCipher([]byte(secret))
	if err != nil {
		return "", errs.Wrap(err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errs.Wrap(err)
	}

	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return "", errs.Wrap(err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errs.Wrap(err)
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errs.Wrap(err)
	}

	return string(plaintext), nil
}

// UUIDToSecret converts a UUIDv4 to a 32 char secret string by
// removing the '-' between the UUID parts
func UUIDToSecret(id *uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")
}
