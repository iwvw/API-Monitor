package m365

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
)

func generateRegistrationPassword() (string, error) {
	const length = 12
	lower := "abcdefghijklmnopqrstuvwxyz"
	upper := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits := "0123456789"
	symbols := "-_@#$%!?"
	password := make([]byte, 0, length)
	for _, charset := range []string{lower, upper, digits, symbols} {
		next, err := randomCharsetByte(charset)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	for len(password) < length {
		next, err := randomCharsetByte(lower + upper + digits + symbols)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	if err := shuffleBytes(password); err != nil {
		return "", err
	}
	return string(password), nil
}

func generateInviteCode() (string, error) {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func generateTemporaryPassword() (string, error) {
	const (
		passwordLength = 18
		upperCharset   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		lowerCharset   = "abcdefghijkmnopqrstuvwxyz"
		digitCharset   = "23456789"
		symbolCharset  = "!@#$%^*-_+=?"
	)
	requiredCharsets := []string{upperCharset, lowerCharset, digitCharset, symbolCharset}
	allCharsets := upperCharset + lowerCharset + digitCharset + symbolCharset
	password := make([]byte, 0, passwordLength)

	for _, charset := range requiredCharsets {
		next, err := randomCharsetByte(charset)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	for len(password) < passwordLength {
		next, err := randomCharsetByte(allCharsets)
		if err != nil {
			return "", err
		}
		password = append(password, next)
	}
	if err := shuffleBytes(password); err != nil {
		return "", err
	}
	return string(password), nil
}

func randomCharsetByte(charset string) (byte, error) {
	if charset == "" {
		return 0, errors.New("empty charset")
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	if err != nil {
		return 0, err
	}
	return charset[index.Int64()], nil
}

func shuffleBytes(items []byte) error {
	for i := len(items) - 1; i > 0; i-- {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(index.Int64())
		items[i], items[j] = items[j], items[i]
	}
	return nil
}

func isPasswordComplexityError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "password complexity") ||
		strings.Contains(message, "password does not comply") ||
		strings.Contains(message, "specified password does not comply")
}
