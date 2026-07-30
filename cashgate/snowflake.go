package cashgate

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	sf "github.com/snowflakedb/gosnowflake/v2"
)

// SnowflakeConfig holds the settings for a JWT-authenticated Snowflake
// connection. Warehouse is optional when the Snowflake user or role has a
// usable default warehouse.
type SnowflakeConfig struct {
	Account    string
	User       string
	PrivateKey string // Base64-encoded PEM private key.
	Database   string
	Schema     string
	Warehouse  string
	Port       int
}

// NewSnowflakeConnection opens and verifies a gosnowflake v2 connection.
//
// Account, User, PrivateKey, Database, and Schema are all required. Requiring a
// complete configuration prevents a partially configured deployment from
// silently running without the cash gate.
func NewSnowflakeConnection(cfg SnowflakeConfig) (*sql.DB, error) {
	cfg.Account = strings.TrimSpace(cfg.Account)
	cfg.User = strings.TrimSpace(cfg.User)
	cfg.PrivateKey = strings.TrimSpace(cfg.PrivateKey)
	cfg.Database = strings.ToUpper(strings.TrimSpace(cfg.Database))
	cfg.Schema = strings.ToUpper(strings.TrimSpace(cfg.Schema))
	cfg.Warehouse = strings.TrimSpace(cfg.Warehouse)

	var missing []string
	if cfg.Account == "" {
		missing = append(missing, "account")
	}
	if cfg.User == "" {
		missing = append(missing, "user")
	}
	if cfg.PrivateKey == "" {
		missing = append(missing, "private key")
	}
	if cfg.Database == "" {
		missing = append(missing, "database")
	}
	if cfg.Schema == "" {
		missing = append(missing, "schema")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("cashgate: incomplete Snowflake configuration; missing %s", strings.Join(missing, ", "))
	}
	if err := validateSnowflakeIdentifier("database", cfg.Database); err != nil {
		return nil, err
	}
	if err := validateSnowflakeIdentifier("schema", cfg.Schema); err != nil {
		return nil, err
	}
	if cfg.Port < 0 {
		return nil, fmt.Errorf("cashgate: Snowflake port must not be negative")
	}

	privateKeyDER, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("cashgate: decode Snowflake private key from base64: %w", err)
	}
	privateKey, err := parseRSAPrivateKey(privateKeyDER)
	if err != nil {
		return nil, err
	}

	port := cfg.Port
	if port == 0 {
		port = 443
	}
	dsn, err := sf.DSN(&sf.Config{
		Account:       cfg.Account,
		User:          cfg.User,
		Port:          port,
		Authenticator: sf.AuthTypeJwt,
		PrivateKey:    privateKey,
		Database:      cfg.Database,
		Schema:        cfg.Schema,
		Warehouse:     cfg.Warehouse,
	})
	if err != nil {
		return nil, fmt.Errorf("cashgate: build Snowflake DSN: %w", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("cashgate: open Snowflake connection: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cashgate: verify Snowflake connection: %w", err)
	}
	return db, nil
}

func parseRSAPrivateKey(privateKeyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("cashgate: Snowflake private key is not PEM encoded")
	}

	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("cashgate: parse PKCS8 Snowflake private key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("cashgate: Snowflake private key is not RSA")
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("cashgate: parse PKCS1 Snowflake private key: %w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("cashgate: unsupported Snowflake private key PEM type %q", block.Type)
	}
}
