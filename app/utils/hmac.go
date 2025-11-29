package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func VerifyHMAC(message, secret, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(message))
    expected := hex.EncodeToString(mac.Sum(nil))

    return hmac.Equal([]byte(expected), []byte(signature))
}
