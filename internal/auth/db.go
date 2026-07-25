package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists      = errors.New("username already exists")
	ErrInvalidLogin    = errors.New("invalid username or password")
	ErrBadInput        = errors.New("invalid username or password format")
	ErrUserNotFound    = errors.New("user not found")
)

type User struct {
	ID       int64
	Username string
	Nickname string
	Gold     int
}

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "mahjong.db"
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  nickname TEXT NOT NULL,
  gold INTEGER NOT NULL DEFAULT 10000,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
`)
	return err
}

func normalizeUser(u string) string {
	return strings.TrimSpace(u)
}

func validUsername(u string) bool {
	if len(u) < 3 || len(u) > 24 {
		return false
	}
	for _, r := range u {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validPassword(p string) bool {
	return len(p) >= 4 && len(p) <= 64
}

func (s *Store) Register(username, password, nickname string) (*User, error) {
	username = normalizeUser(username)
	password = strings.TrimSpace(password)
	nickname = strings.TrimSpace(nickname)
	if !validUsername(username) || !validPassword(password) {
		return nil, ErrBadInput
	}
	if nickname == "" {
		nickname = username
	}
	if len(nickname) > 24 {
		nickname = nickname[:24]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`INSERT INTO users(username, password_hash, nickname, gold, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		username, string(hash), nickname, 10000, now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrUserExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, Nickname: nickname, Gold: 10000}, nil
}

func (s *Store) Login(username, password string) (*User, error) {
	username = normalizeUser(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return nil, ErrInvalidLogin
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	var hash string
	err := s.db.QueryRow(
		`SELECT id, username, nickname, gold, password_hash FROM users WHERE username = ? COLLATE NOCASE`,
		username,
	).Scan(&u.ID, &u.Username, &u.Nickname, &u.Gold, &hash)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidLogin
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidLogin
	}
	return &u, nil
}

func (s *Store) GetByID(id int64) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	err := s.db.QueryRow(
		`SELECT id, username, nickname, gold FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Nickname, &u.Gold)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) SetGold(userID int64, gold int) error {
	if gold < 0 {
		gold = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`UPDATE users SET gold = ?, updated_at = ? WHERE id = ?`, gold, now, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Store) AddGold(userID int64, delta int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var gold int
	err := s.db.QueryRow(`SELECT gold FROM users WHERE id = ?`, userID).Scan(&gold)
	if err == sql.ErrNoRows {
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, err
	}
	gold += delta
	if gold < 0 {
		gold = 0
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE users SET gold = ?, updated_at = ? WHERE id = ?`, gold, now, userID)
	return gold, err
}

func (s *Store) SetNickname(userID int64, nickname string) error {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return fmt.Errorf("empty nickname")
	}
	if len(nickname) > 24 {
		nickname = nickname[:24]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE users SET nickname = ?, updated_at = ? WHERE id = ?`, nickname, now, userID)
	return err
}