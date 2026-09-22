package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type AppError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return e.Code + ": " + e.Message
}

func E(status int, code, message string, err ...error) *AppError {
	e := &AppError{Status: status, Code: code, Message: message}
	if len(err) > 0 {
		e.Err = err[0]
	}
	return e
}

func AsAppError(err error) *AppError {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return E(500, "INTERNAL_ERROR", "Something went wrong.", err)
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func HashToken(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func NormalizePlate(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func ValidEmail(s string) bool { return len(s) <= 254 && emailPattern.MatchString(s) }
