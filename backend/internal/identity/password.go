package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB   uint32 = 19 * 1024
	argonIterations  uint32 = 2
	argonParallelism uint8  = 1
	saltBytes               = 16
	keyBytes                = 32
	maxPasswordBytes        = 1 << 20
)

// PasswordHasher implements versioned Argon2id password encoding. The current
// parameters are 19 MiB memory, 2 iterations, one lane, 16-byte salt, and a
// 32-byte derived key. These are explicit operational defaults and may be
// adjusted after load testing; encoded hashes preserve their parameters.
type PasswordHasher struct{}

func (PasswordHasher) Hash(password string) (string, error) {
	if password == "" || len(password) > maxPasswordBytes {
		return "", errors.New("password must be non-empty and at most 1 MiB")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, keyBytes)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemoryKiB, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (PasswordHasher) Verify(encoded, password string) (bool, error) {
	if password == "" || len(password) > maxPasswordBytes {
		return false, errors.New("password must be non-empty and at most 1 MiB")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("unsupported Argon2id hash encoding")
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 || !strings.HasPrefix(params[0], "m=") || !strings.HasPrefix(params[1], "t=") || !strings.HasPrefix(params[2], "p=") {
		return false, errors.New("invalid Argon2id parameters")
	}
	memoryValue, err := strconv.ParseUint(strings.TrimPrefix(params[0], "m="), 10, 32)
	if err != nil {
		return false, errors.New("invalid Argon2id memory parameter")
	}
	iterationValue, err := strconv.ParseUint(strings.TrimPrefix(params[1], "t="), 10, 32)
	if err != nil {
		return false, errors.New("invalid Argon2id iteration parameter")
	}
	parallelValue, err := strconv.ParseUint(strings.TrimPrefix(params[2], "p="), 10, 8)
	if err != nil {
		return false, errors.New("invalid Argon2id parallelism parameter")
	}
	memory, iterations, parallelism := uint32(memoryValue), uint32(iterationValue), uint8(parallelValue)
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return false, errors.New("Argon2id parameters outside accepted bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false, errors.New("invalid Argon2id key")
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func sessionTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
