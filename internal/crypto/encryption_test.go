// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package crypto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt_Success(t *testing.T) {
	key, err := GenerateKey()
	require.NoError(t, err)

	tests := []struct {
		name string
		pat  string
	}{
		{"simple token", "ghp_1234567890abcdefghijklmnopqrstuv"},
		{"empty string", ""},
		{"special chars", "token!@#$%^&*()_+-={}[]|\\:\";<>?,./"},
		{"unicode", "token_🔐_secure_🔑"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := EncryptPAT(tt.pat, key)
			require.NoError(t, err)
			assert.NotEmpty(t, encrypted)
			assert.NotEqual(t, tt.pat, encrypted)

			decrypted, err := DecryptPAT(encrypted, key)
			require.NoError(t, err)
			assert.Equal(t, tt.pat, decrypted)
		})
	}
}

func TestEncrypt_InvalidKey(t *testing.T) {
	tests := []struct {
		name    string
		keySize int
	}{
		{"too short", 8},
		{"odd size", 15},
		{"invalid size", 31},
		{"too long", 33},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			encrypted, err := EncryptPAT("test-token", key)
			assert.Error(t, err)
			assert.Equal(t, ErrInvalidKey, err)
			assert.Empty(t, encrypted)
		})
	}
}

func TestDecrypt_InvalidKey(t *testing.T) {
	key, _ := GenerateKey()
	encrypted, _ := EncryptPAT("test-token", key)

	invalidKey := make([]byte, 8)
	decrypted, err := DecryptPAT(encrypted, invalidKey)
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidKey, err)
	assert.Empty(t, decrypted)
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	encrypted, err := EncryptPAT("test-token", key1)
	require.NoError(t, err)

	decrypted, err := DecryptPAT(encrypted, key2)
	assert.Error(t, err)
	assert.Empty(t, decrypted)
}

func TestDecrypt_InvalidCiphertext(t *testing.T) {
	key, _ := GenerateKey()

	tests := []struct {
		name       string
		ciphertext string
	}{
		{"not base64", "not-base64!@#"},
		{"empty", ""},
		{"too short", "AA=="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decrypted, err := DecryptPAT(tt.ciphertext, key)
			assert.Error(t, err)
			assert.Empty(t, decrypted)
		})
	}
}

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	require.NoError(t, err)
	assert.Len(t, key1, 32)

	key2, err := GenerateKey()
	require.NoError(t, err)
	assert.Len(t, key2, 32)

	// Keys should be different (extremely unlikely to be same)
	assert.NotEqual(t, key1, key2)
}

func TestKeyConversion(t *testing.T) {
	key, err := GenerateKey()
	require.NoError(t, err)

	// Convert to string
	encoded := KeyToString(key)
	assert.NotEmpty(t, encoded)

	// Convert back to key
	decoded, err := StringToKey(encoded)
	require.NoError(t, err)
	assert.Equal(t, key, decoded)
}

func TestStringToKey_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
	}{
		{"not base64", "not-base64!@#"},
		{"wrong size", KeyToString(make([]byte, 8))},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := StringToKey(tt.encoded)
			assert.Error(t, err)
			assert.Nil(t, key)
		})
	}
}

func TestEncrypt_DifferentNonces(t *testing.T) {
	key, _ := GenerateKey()
	pat := "test-token"

	// Encrypt same token twice
	encrypted1, err1 := EncryptPAT(pat, key)
	require.NoError(t, err1)

	encrypted2, err2 := EncryptPAT(pat, key)
	require.NoError(t, err2)

	// Encrypted values should be different (due to different nonces)
	assert.NotEqual(t, encrypted1, encrypted2)

	// But both should decrypt to the same value
	decrypted1, _ := DecryptPAT(encrypted1, key)
	decrypted2, _ := DecryptPAT(encrypted2, key)
	assert.Equal(t, pat, decrypted1)
	assert.Equal(t, pat, decrypted2)
}

func TestEncryptPAT_ValidKeySizes(t *testing.T) {
	tests := []struct {
		name    string
		keySize int
	}{
		{"16 bytes (AES-128)", 16},
		{"24 bytes (AES-192)", 24},
		{"32 bytes (AES-256)", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			for i := range key {
				key[i] = byte(i)
			}

			encrypted, err := EncryptPAT("test-token", key)
			require.NoError(t, err)
			assert.NotEmpty(t, encrypted)

			decrypted, err := DecryptPAT(encrypted, key)
			require.NoError(t, err)
			assert.Equal(t, "test-token", decrypted)
		})
	}
}

func TestEncryptPAT_LongToken(t *testing.T) {
	key, _ := GenerateKey()
	
	// Create a very long token
	longToken := strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789", 100)

	encrypted, err := EncryptPAT(longToken, key)
	require.NoError(t, err)

	decrypted, err := DecryptPAT(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, longToken, decrypted)
}
