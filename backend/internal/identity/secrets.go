package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
)

var (
	ErrMFAUnavailable = errors.New("MFA encryption is not configured")
	ErrInvalidMFAKey  = errors.New("MFA encryption key must be 32 bytes")
)

// SecretBox encrypts short authentication secrets with AES-256-GCM. The
// additional data binds a ciphertext to its owner and purpose.
type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, ErrInvalidMFAKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) Encrypt(plaintext []byte, additionalData string) (ciphertext, nonce []byte, err error) {
	if b == nil || b.aead == nil {
		return nil, nil, ErrMFAUnavailable
	}
	nonce = make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = b.aead.Seal(nil, nonce, plaintext, []byte(additionalData))
	return ciphertext, nonce, nil
}

func (b *SecretBox) Decrypt(ciphertext, nonce []byte, additionalData string) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, ErrMFAUnavailable
	}
	if len(nonce) != b.aead.NonceSize() {
		return nil, errors.New("invalid encrypted secret nonce")
	}
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, []byte(additionalData))
	if err != nil {
		return nil, errors.New("encrypted secret authentication failed")
	}
	return plaintext, nil
}

func randomBytes(size int) ([]byte, error) {
	if size < 1 {
		return nil, errors.New("random byte count must be positive")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return buf, nil
}

func randomToken(size int) (string, error) {
	buf, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func recoveryCodeHash(pepper []byte, code string) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(mac.Sum(nil))
}

// NewTOTPSecret returns the raw 20-byte secret and its user-facing Base32 form.
func NewTOTPSecret() ([]byte, string, error) {
	secret, err := randomBytes(20)
	if err != nil {
		return nil, "", err
	}
	return secret, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

func decodeTOTPSecret(encoded string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(encoded), " ", ""))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
}

func totpStep(at time.Time) int64 {
	return at.UTC().Unix() / int64(totpPeriod/time.Second)
}

func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (int(sum[offset])&0x7f)<<24 |
		int(sum[offset+1])<<16 |
		int(sum[offset+2])<<8 |
		int(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

// VerifyTOTP accepts the current and adjacent 30-second steps. It returns the
// matched step so the repository can atomically reject a replay.
func VerifyTOTP(secret []byte, code string, at time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	current := totpStep(at)
	for offset := int64(-1); offset <= 1; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		expected := totpCode(secret, step)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func totpProvisioningURI(issuer, account, encodedSecret string) string {
	label := issuer + ":" + account
	return "otpauth://totp/" + pathEscape(label) +
		"?secret=" + encodedSecret +
		"&issuer=" + pathEscape(issuer) +
		"&algorithm=SHA1&digits=6&period=30"
}

func pathEscape(value string) string {
	// url.PathEscape leaves ':' unescaped, which is valid in the label.
	return strings.ReplaceAll(strings.ReplaceAll(value, " ", "%20"), "/", "%2F")
}

// ParseSecretKey accepts a 32-byte key encoded as hex or standard/URL base64.
// It rejects anything else so deployments fail closed instead of truncating.
func ParseSecretKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("secret key is empty")
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, errors.New("secret key must encode exactly 32 bytes as hex or base64")
}
