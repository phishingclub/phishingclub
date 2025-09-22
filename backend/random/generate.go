package random

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/phishingclub/phishingclub/errs"
)

// GenerateRandomLowerAndUpper generates random lower and upper case string
func GenerateRandomURLBase64Encoded(length int) (string, error) {
	randomBytes, err := GenerateRandomBytes(length)
	if err != nil {
		return "", fmt.Errorf("failed to generate random string with random bytes: %w", err)
	}
	str := base64.URLEncoding.EncodeToString(randomBytes)
	return str[:length], nil
}

// GenerateRandomBytes generates random bytes
func GenerateRandomBytes(length int) ([]byte, error) {
	buff := make([]byte, length)
	_, err := rand.Read(buff)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return buff, nil
}

// RandomIntN generates a random number between 0 and n
func RandomIntN(n int) (int, error) {
	max := big.NewInt(int64(n))
	randNum, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return int(randNum.Int64()), nil
}

// GenerateRandomCookieName generates a random cookie name with length between 8-16 characters
func GenerateRandomCookieName() (string, error) {
	// generate random length between 8 and 16
	length, err := RandomIntN(9) // 0-8, add 8 to get 8-16
	if err != nil {
		return "", fmt.Errorf("failed to generate random cookie name length: %w", err)
	}
	length += 8 // now 8-16

	return GenerateRandomURLBase64Encoded(length)
}
