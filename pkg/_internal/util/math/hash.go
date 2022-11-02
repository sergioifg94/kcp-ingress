package math

import (
	"crypto/sha256"
	"math/big"
)

// HashString function can be used to hash a string
func HashString(aString string) string {
	hash := sha256.Sum224([]byte(aString))
	base62hash := toBase62(hash)
	return base62hash
}

func toBase62(hash [28]byte) string {
	var i big.Int
	i.SetBytes(hash[:])
	return i.Text(62)
}
