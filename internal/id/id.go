package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

func New(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return strings.TrimSuffix(prefix, "_") + "_" + hex.EncodeToString(raw[:])
}
