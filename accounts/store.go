package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const minimumHMACSecretBytes = 32

const (
	DefaultTermsVersion  = "2026-08-16"
	DefaultRefundVersion = "2026-08-16"
)

type Config struct {
	HMACSecret    []byte
	TermsVersion  string
	RefundVersion string
	Now           func() time.Time
	Random        io.Reader
}

type Store struct {
	db            *sql.DB
	writeMu       sync.Mutex
	hmacSecret    []byte
	now           func() time.Time
	random        io.Reader
	termsVersion  string
	refundVersion string
}

func Open(ctx context.Context, path string, config Config) (*Store, error) {
	if ctx == nil || strings.TrimSpace(path) == "" || len(config.HMACSecret) < minimumHMACSecretBytes {
		return nil, ErrInvalidInput
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if strings.TrimSpace(config.TermsVersion) == "" {
		config.TermsVersion = DefaultTermsVersion
	}
	if strings.TrimSpace(config.RefundVersion) == "" {
		config.RefundVersion = DefaultRefundVersion
	}
	if len(config.TermsVersion) > 64 || len(config.RefundVersion) > 64 {
		return nil, ErrInvalidInput
	}
	databasePath, err := prepareDatabasePath(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("accounts: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{
		db: db, hmacSecret: append([]byte(nil), config.HMACSecret...),
		now: config.Now, random: config.Random, termsVersion: config.TermsVersion, refundVersion: config.RefundVersion,
	}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func prepareDatabasePath(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("accounts: resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", fmt.Errorf("accounts: create database directory: %w", err)
	}
	info, err := os.Lstat(abs)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: accounts database must be a regular file, not a symlink", ErrInvalidInput)
		}
	case errors.Is(err, os.ErrNotExist):
		file, createErr := os.OpenFile(abs, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return "", fmt.Errorf("accounts: create database: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", fmt.Errorf("accounts: close new database: %w", closeErr)
		}
	default:
		return "", fmt.Errorf("accounts: inspect database: %w", err)
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		return "", fmt.Errorf("accounts: secure database: %w", err)
	}
	return abs, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("accounts: initialize database: %w", err)
		}
	}
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("accounts: begin migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("accounts: create migration ledger: %w", err)
	}
	var currentVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&currentVersion); err != nil {
		return fmt.Errorf("accounts: read migration version: %w", err)
	}
	if len(schemaMigrations) == 0 || currentVersion > schemaMigrations[len(schemaMigrations)-1].version {
		return fmt.Errorf("accounts: database schema version %d is newer than this binary", currentVersion)
	}
	for _, migration := range schemaMigrations {
		if migration.version <= currentVersion {
			continue
		}
		if migration.version != currentVersion+1 {
			return fmt.Errorf("accounts: non-contiguous migration %d after %d", migration.version, currentVersion)
		}
		for _, statement := range migration.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("accounts: apply migration %d: %w", migration.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, migration.version, store.now().UTC().Unix()); err != nil {
			return fmt.Errorf("accounts: record migration %d: %w", migration.version, err)
		}
		currentVersion = migration.version
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("accounts: commit migration: %w", err)
	}
	return nil
}

func (store *Store) Ping(ctx context.Context) error {
	if store == nil || store.db == nil || ctx == nil {
		return ErrInvalidInput
	}
	return store.db.PingContext(ctx)
}

func (store *Store) withWriteTx(ctx context.Context, operation func(*sql.Tx) error) error {
	if store == nil || store.db == nil || ctx == nil || operation == nil {
		return ErrInvalidInput
	}
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) randomToken(bytes int) (string, error) {
	if bytes < 16 {
		return "", ErrInvalidInput
	}
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(store.random, buffer); err != nil {
		return "", fmt.Errorf("accounts: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (store *Store) newID(prefix string) (string, error) {
	token, err := store.randomToken(16)
	if err != nil {
		return "", err
	}
	return prefix + token, nil
}

func (store *Store) digest(value string) []byte {
	mac := hmac.New(sha256.New, store.hmacSecret)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (store *Store) csrfToken(sessionToken string) string {
	mac := hmac.New(sha256.New, store.hmacSecret)
	_, _ = mac.Write([]byte("csrf:" + sessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func constantTimeEqual(left, right []byte) bool {
	return len(left) == len(right) && hmac.Equal(left, right)
}

func unixTime(value int64) time.Time { return time.Unix(value, 0).UTC() }

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := unixTime(value.Int64)
	return &timestamp
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func translateConstraint(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") || strings.Contains(message, "constraint failed") {
		return fmt.Errorf("%w: database constraint", ErrConflict)
	}
	return err
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func hexDigest(value []byte) string { return hex.EncodeToString(value) }
