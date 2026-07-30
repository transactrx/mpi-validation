package cashgate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func TestNewSnowflakeConnectionRejectsIncompleteConfiguration(t *testing.T) {
	_, err := NewSnowflakeConnection(SnowflakeConfig{})
	if err == nil {
		t.Fatal("empty SnowflakeConfig must fail")
	}
	for _, field := range []string{"account", "user", "private key", "database", "schema"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("incomplete configuration error %q does not identify missing %q", err, field)
		}
	}

	_, err = NewSnowflakeConnection(SnowflakeConfig{
		Account:    "account",
		User:       "user",
		PrivateKey: "not-base64",
		Database:   "CPE_PROD",
		Schema:     "DATA_DICTIONARY",
	})
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("malformed key error = %v, want base64 failure", err)
	}
}

func TestNewSnowflakeConnectionValidatesLocationAndPortBeforeConnecting(t *testing.T) {
	validButNotPEM := base64.StdEncoding.EncodeToString([]byte("not PEM"))
	tests := []struct {
		name   string
		config SnowflakeConfig
		want   string
	}{
		{
			name: "database identifier",
			config: SnowflakeConfig{
				Account:    "account",
				User:       "user",
				PrivateKey: validButNotPEM,
				Database:   "CPE-PROD",
				Schema:     "DATA_DICTIONARY",
			},
			want: "not a valid",
		},
		{
			name: "negative port",
			config: SnowflakeConfig{
				Account:    "account",
				User:       "user",
				PrivateKey: validButNotPEM,
				Database:   "CPE_PROD",
				Schema:     "DATA_DICTIONARY",
				Port:       -1,
			},
			want: "port",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSnowflakeConnection(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestParseRSAPrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER})
	gotPKCS8, err := parseRSAPrivateKey(pkcs8PEM)
	if err != nil {
		t.Fatalf("parse PKCS8 key: %v", err)
	}
	if gotPKCS8.N.Cmp(key.N) != 0 {
		t.Fatal("parsed PKCS8 key differs from source")
	}

	pkcs1PEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	gotPKCS1, err := parseRSAPrivateKey(pkcs1PEM)
	if err != nil {
		t.Fatalf("parse PKCS1 key: %v", err)
	}
	if gotPKCS1.N.Cmp(key.N) != 0 {
		t.Fatal("parsed PKCS1 key differs from source")
	}
}

func TestParseRSAPrivateKeyRejectsInvalidAndNonRSAKeys(t *testing.T) {
	if _, err := parseRSAPrivateKey(nil); err == nil {
		t.Fatal("nil key must fail")
	}
	if _, err := parseRSAPrivateKey([]byte("not PEM")); err == nil {
		t.Fatal("non-PEM key must fail")
	}

	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ecdsaKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	ecdsaPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, err := parseRSAPrivateKey(ecdsaPEM); err == nil || !strings.Contains(err.Error(), "not RSA") {
		t.Fatalf("non-RSA error = %v", err)
	}

	unsupportedPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("invalid")})
	if _, err := parseRSAPrivateKey(unsupportedPEM); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported PEM error = %v", err)
	}
}
