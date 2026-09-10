package chat

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct{ Read, Write *sql.DB }

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(abs)
	if filepath.VolumeName(abs) != "" {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	dsn := u.String() + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2048)&_txlock=immediate"
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if _, err = w.Exec(schema); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	r.SetMaxIdleConns(4)
	return &Store{Read: r, Write: w}, nil
}
func (s *Store) Close() { s.Read.Close(); s.Write.Close() }
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func now() int64 { return time.Now().UnixMilli() }
func pair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}
func passwordHash(password string) string {
	salt := id()
	key, err := pbkdf2.Key(sha256.New, password, []byte(salt), 210000, 32)
	if err != nil {
		panic(err)
	}
	return salt + ":" + hex.EncodeToString(key)
}
func passwordMatches(hash, password string) bool {
	parts := strings.Split(hash, ":")
	if len(parts) != 2 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, []byte(parts[0]), 210000, 32)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(key)), []byte(parts[1])) == 1
}

type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	Online    bool   `json:"online"`
	CreatedAt int64  `json:"created_at"`
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Disabled, &u.CreatedAt)
	return u, err
}

const userCols = "id,username,name,role,disabled,created_at"

type problem struct {
	status  int
	message string
}

func (e *problem) Error() string            { return e.message }
func fail(status int, message string) error { return &problem{status, message} }
func exists(tx *sql.Tx, q string, args ...any) bool {
	var n int
	return tx.QueryRow(q, args...).Scan(&n) == nil && n > 0
}
func membership(tx *sql.Tx, room, user string) error {
	if !exists(tx, "SELECT COUNT(*) FROM members WHERE room_id=? AND user_id=?", room, user) {
		return fail(403, "你不在此会话中")
	}
	return nil
}
func capacity(tx *sql.Tx, user string) error {
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM members WHERE user_id=?", user).Scan(&n); err != nil {
		return err
	}
	if n >= 200 {
		return fail(409, "每个账号最多加入 200 个会话")
	}
	return nil
}
func audit(tx *sql.Tx, actor, action, target string) error {
	_, err := tx.Exec("INSERT INTO audit(actor,action,target,created_at) VALUES(?,?,?,?)", actor, action, target, now())
	return err
}
func normalizeDBError(err error) error {
	if err == nil {
		return nil
	}
	var p *problem
	if errors.As(err, &p) {
		return err
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fail(409, "记录已存在，请刷新后重试")
	}
	return fmt.Errorf("database: %w", err)
}
