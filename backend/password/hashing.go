package password

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/random"
	"golang.org/x/crypto/argon2"
)

const (
	// DummyHash is used for negating timing attacks on login.
	// If the user it not found, this hash is used to compare the password against.
	// This will ensure that the response time is closer to the response time when the user is found.
	// If the DefaultArgon* values are changed, this value must be updated.
	DummyHash = "argon2id$v=19$m=65536,t=2,p=1$yUvXmqefdwzTurx0pMH3i4NLaHJ57sdkIvrHtWwuh7I$rWsAgfbeXsNzU5+CTtJ5oelaS+YDlgO3UTqna/jZFskWZAojCGjuAV8KYHHrztYy9/FbFNsdvyOrujNzqGFCWQ"
)

const (
	DefaultArgon2Memory      = 64 * 1024
	DefaultArgon2Iterations  = 2
	DefaultArgon2Parallelism = 1
	DefaultArgon2KeyLen      = 64
)

// Argon2Hasher is a hasher for password.
type Argon2Hasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLen      uint32
}

// NewHasher creates a new hasher.
func NewHasher(memory uint32, iterations uint32, parallelism uint8, keyLen uint32) *Argon2Hasher {
	return &Argon2Hasher{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		keyLen:      keyLen,
	}
}

// NewHasherWithDefaultValues creates a new hasher with default values.
func NewHasherWithDefaultValues() *Argon2Hasher {
	return &Argon2Hasher{
		memory:      DefaultArgon2Memory,
		iterations:  DefaultArgon2Iterations,
		parallelism: DefaultArgon2Parallelism,
		keyLen:      DefaultArgon2KeyLen,
	}
}

// Hash hashes the password using Argon2.
func (h *Argon2Hasher) Hash(password string) (string, error) {
	saltBytes, err := random.GenerateRandomBytes(32)
	if err != nil {
		return "", errs.Wrap(err)
	}
	passwordBytes := []byte(password)
	hashBytes := argon2.IDKey(passwordBytes, saltBytes, h.iterations, h.memory, h.parallelism, h.keyLen)
	hashString := fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(saltBytes),
		base64.RawStdEncoding.EncodeToString(hashBytes))
	return hashString, nil
}

type Argon2Params struct {
	Time        uint32
	Memory      uint32
	Parallelism uint8
	KeyLen      uint32
}
type Argon2Verifier struct{}

func NewArgon2Verifier() *Argon2Verifier {
	return &Argon2Verifier{}
}

// Verify verifies a password against an Argon2 hash.
func (v *Argon2Verifier) Verify(password string, encodedHash string) (bool, error) {
	params, salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return false, errs.Wrap(err)
	}
	computedHash := argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Parallelism, params.KeyLen)
	return subtle.ConstantTimeCompare(hash, computedHash) == 1, nil
}

// decodeHash decodes an Argon2 hash string.
func decodeHash(encodedHash string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 5 {
		return Argon2Params{}, nil, nil, fmt.Errorf("invalid hash format: '%s' - has %d parts", encodedHash, len(parts))
	}

	params := Argon2Params{}
	var err error

	paramParts := strings.Split(parts[2], ",")
	if len(paramParts) != 3 {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to parse parameters")
	}

	memoryVal, err := strconv.ParseUint(paramParts[0][2:], 10, 32)
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to parse memory")
	}
	params.Memory = uint32(memoryVal)

	timeVal, err := strconv.ParseUint(paramParts[1][2:], 10, 32)
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to parse time")
	}
	params.Time = uint32(timeVal)

	parallelismVal, err := strconv.ParseUint(paramParts[2][2:], 10, 8)
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to parse parallelism")
	}
	params.Parallelism = uint8(parallelismVal)

	params.KeyLen = uint32(DefaultArgon2KeyLen)

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to decode salt")
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash format: failed to decode hash")
	}

	return params, salt, hash, nil
}
