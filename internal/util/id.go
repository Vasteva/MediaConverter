package util

import (
	"crypto/rand"
	"math/big"
	"time"
)

// GenerateID returns a time-prefixed unique ID (e.g. "20060102150405-ab3x9z").
func GenerateID() string {
	return time.Now().Format("20060102150405") + "-" + RandomString(6)
}

// RandomString returns a cryptographically random lowercase alphanumeric string of length n.
func RandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			b[i] = letters[i%len(letters)]
			continue
		}
		b[i] = letters[idx.Int64()]
	}
	return string(b)
}
