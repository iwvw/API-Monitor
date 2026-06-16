package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const defaultEncryptKey = "default-encryption-key-change-in-production-32bytes"

var encryptedPattern = regexp.MustCompile(`^[0-9a-fA-F]+:[0-9a-fA-F]+:[0-9a-fA-F]+$`)

func IsEncrypted(value string) bool {
	parts := strings.Split(value, ":")
	return len(parts) == 3 && encryptedPattern.MatchString(value)
}

func SecureEncrypt(value string) (string, error) {
	if value == "" || IsEncrypted(value) {
		return value, nil
	}
	return EncryptNodeGCM(value)
}

func SecureDecrypt(value string) string {
	if value == "" || !IsEncrypted(value) {
		return value
	}
	decrypted, err := DecryptNodeGCM(value)
	if err != nil {
		return value
	}
	return decrypted
}

func EncryptJSON(value interface{}) (string, error) {
	serialized, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return SecureEncrypt(string(serialized))
}

func DecryptJSON(value string, target interface{}) error {
	plain := SecureDecrypt(value)
	return json.Unmarshal([]byte(plain), target)
}

func DecryptNodeGCM(encrypted string) (string, error) {
	parts := strings.Split(encrypted, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("encrypted value has invalid format")
	}
	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode iv: %w", err)
	}
	authTag, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode auth tag: %w", err)
	}
	cipherText, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(encryptionKey())
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	payload := append(cipherText, authTag...)
	plain, err := gcm.Open(nil, iv, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func EncryptNodeGCM(plain string) (string, error) {
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}
	block, err := aes.NewCipher(encryptionKey())
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	tagStart := len(sealed) - gcm.Overhead()
	cipherText := sealed[:tagStart]
	authTag := sealed[tagStart:]
	return fmt.Sprintf("%s:%s:%s", hex.EncodeToString(iv), hex.EncodeToString(authTag), hex.EncodeToString(cipherText)), nil
}

func encryptionKey() []byte {
	keySource := os.Getenv("ENCRYPTION_KEY")
	if keySource == "" {
		keySource = defaultEncryptKey
	}
	key := sha256.Sum256([]byte(keySource))
	return key[:]
}
