package chat

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	SetupToken, Origin string
	SecureCookie       bool
}
type Server struct {
	store     *Store
	hub       *Hub
	config    Config
	started   time.Time
	limits    limiter
	authSlots chan struct{}
	stop      chan struct{}
	dummyHash string
}
type userKey struct{}

func current(r *http.Request) User { return r.Context().Value(userKey{}).(User) }
func New(store *Store, config Config) *Server {
	return &Server{store: store, hub: newHub(), config: config, started: time.Now(), authSlots: make(chan struct{}, 4), stop: make(chan struct{}), dummyHash: passwordHash(id())}
}
func (s *Server) Shutdown() { close(s.stop) }

type endpoint func(http.ResponseWriter, *http.Request) error

func (s *Server) Handler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	add := func(pattern string, auth, admin bool, fn endpoint) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if auth {
				u, err := s.authenticate(r)
				if err != nil {
					writeError(w, fail(401, "请先登录"))
					return
				}
				if admin && u.Role != "admin" {
					writeError(w, fail(403, "需要管理员权限"))
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
				if r.Method != "GET" && !s.limits.allow("user:"+u.ID, 30, 5) {
					writeError(w, fail(429, "操作太快，请稍后重试"))
					return
				}
			}
			if err := fn(w, r); err != nil {
				writeError(w, normalizeDBError(err))
			}
		})
	}
	add("GET /api/status", false, false, s.status)
	add("POST /api/setup", false, false, s.signup)
	add("POST /api/register", false, false, s.signup)
	add("POST /api/login", false, false, s.login)
	add("POST /api/logout", true, false, s.logout)
	add("GET /api/me", true, false, func(w http.ResponseWriter, r *http.Request) error { return jsonResponse(w, current(r)) })
	add("PATCH /api/me", true, false, s.profile)
	add("GET /api/users", true, false, s.searchUsers)
	add("GET /api/friends", true, false, s.friends)
	add("POST /api/friends/requests", true, false, s.requestFriend)
	add("POST /api/friends/requests/{id}", true, false, s.resolveRequest)
	add("DELETE /api/friends/{id}", true, false, s.removeFriend)
	add("GET /api/rooms", true, false, s.rooms)
	add("POST /api/rooms", true, false, s.createGroup)
	add("GET /api/rooms/{id}/members", true, false, s.members)
	add("POST /api/rooms/{id}/members", true, false, s.addMember)
	add("DELETE /api/rooms/{id}/members/{user}", true, false, s.removeMember)
	add("PATCH /api/rooms/{id}", true, false, s.updateRoom)
	add("GET /api/rooms/{id}/messages", true, false, s.messages)
	add("POST /api/rooms/{id}/messages", true, false, s.sendMessage)
	add("POST /api/rooms/{id}/read", true, false, s.markRead)
	add("GET /api/events", true, false, s.events)
	add("GET /api/admin/stats", true, true, s.adminStats)
	add("GET /api/admin/users", true, true, s.adminUsers)
	add("PATCH /api/admin/users/{id}", true, true, s.adminUpdateUser)
	add("GET /api/admin/rooms", true, true, s.adminRooms)
	add("PATCH /api/admin/rooms/{id}", true, true, s.adminUpdateRoom)
	add("GET /api/admin/audit", true, true, s.adminAudit)
	add("PATCH /api/admin/settings", true, true, s.adminSettings)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.store.Read.PingContext(ctx); err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, fail(404, "接口不存在")) })
	if assets != nil {
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.Method != "HEAD" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			http.FileServerFS(assets).ServeHTTP(w, r)
		}))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !s.sameOrigin(r) {
				writeError(w, fail(403, "请求来源不受信任"))
				return
			}
			if r.Method != "DELETE" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeError(w, fail(415, "需要 JSON 请求"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if s.config.Origin != "" {
		return origin == s.config.Origin
	}
	parsed, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && parsed.Scheme == scheme && parsed.Host == r.Host
}
func jsonResponse(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	status, message := 500, "服务器暂时无法完成请求"
	var p *problem
	if errors.As(err, &p) {
		status, message = p.status, p.message
	} else {
		slog.Error("request failed", "error", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if status == 429 {
		w.Header().Set("Retry-After", "3")
	}
	w.WriteHeader(status)
	jsonResponse(w, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fail(400, "请求内容无效或超过 16 KB")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fail(400, "请求格式无效")
	}
	return nil
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (s *Server) authenticate(r *http.Request) (User, error) {
	cookie, err := r.Cookie("oc_session")
	if err != nil {
		return User{}, err
	}
	return scanUser(s.store.Read.QueryRowContext(r.Context(), "SELECT u.id,u.username,u.name,u.role,u.disabled,u.created_at FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token=? AND s.expires_at>? AND u.disabled=0", tokenHash(cookie.Value), now()))
}
func (s *Server) session(w http.ResponseWriter, r *http.Request, user string) error {
	token := id() + id()
	expiry := time.Now().Add(7 * 24 * time.Hour)
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM sessions WHERE expires_at<?", now()); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM sessions WHERE user_id=? AND token NOT IN (SELECT token FROM sessions WHERE user_id=? ORDER BY expires_at DESC LIMIT 9)", user, user); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO sessions VALUES(?,?,?)", tokenHash(token), user, expiry.UnixMilli())
		return err
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "oc_session", Value: token, Path: "/", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: 604800})
	return nil
}
func (s *Server) authGate(r *http.Request) error {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.limits.allow("auth:"+ip, 12, .2) {
		return fail(429, "尝试次数过多，请稍后重试")
	}
	select {
	case s.authSlots <- struct{}{}:
		return nil
	default:
		return fail(429, "登录繁忙，请稍后重试")
	}
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) error {
	var n int
	var registration string
	if err := s.store.Read.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if err := s.store.Read.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE key='registration'").Scan(&registration); err != nil {
		return err
	}
	return jsonResponse(w, map[string]any{"setup_required": n == 0, "registration": registration == "true"})
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,24}$`)

func (s *Server) signup(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Username, Name, Password, Token string }
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !usernamePattern.MatchString(in.Username) || len(strings.TrimSpace(in.Name)) == 0 || len([]rune(in.Name)) > 32 || len(in.Password) < 10 || len(in.Password) > 128 {
		return fail(400, "用户名需 3–24 位字母、数字或下划线；昵称最多 32 字；密码需 10–128 字节")
	}
	if err := s.authGate(r); err != nil {
		return err
	}
	defer func() { <-s.authSlots }()
	setup := r.URL.Path == "/api/setup"
	if setup && subtle.ConstantTimeCompare([]byte(in.Token), []byte(s.config.SetupToken)) != 1 {
		return fail(403, "初始化令牌不正确")
	}
	u := User{ID: id(), Username: strings.ToLower(in.Username), Name: strings.TrimSpace(in.Name), Role: "user", CreatedAt: now()}
	hash := passwordHash(in.Password)
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
			return err
		}
		if setup {
			if count != 0 {
				return fail(409, "初始化已完成")
			}
			u.Role = "admin"
		} else {
			var enabled string
			if err := tx.QueryRow("SELECT value FROM settings WHERE key='registration'").Scan(&enabled); err != nil {
				return err
			}
			if count == 0 || enabled != "true" {
				return fail(403, "当前未开放注册")
			}
		}
		_, err := tx.Exec("INSERT INTO users(id,username,name,password,role,created_at) VALUES(?,?,?,?,?,?)", u.ID, u.Username, u.Name, hash, u.Role, u.CreatedAt)
		return err
	})
	if err != nil {
		return err
	}
	if err = s.session(w, r, u.ID); err != nil {
		return err
	}
	return jsonResponse(w, u)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Username, Password string }
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if len(in.Password) > 128 {
		return fail(400, "密码过长")
	}
	if err := s.authGate(r); err != nil {
		return err
	}
	defer func() { <-s.authSlots }()
	var hash string
	u, err := scanUser(s.store.Read.QueryRowContext(r.Context(), "SELECT "+userCols+" FROM users WHERE username=?", in.Username))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		hash = s.dummyHash
	} else {
		if err = s.store.Read.QueryRowContext(r.Context(), "SELECT password FROM users WHERE id=?", u.ID).Scan(&hash); err != nil {
			return err
		}
	}
	if !passwordMatches(hash, in.Password) || u.ID == "" || u.Disabled {
		return fail(401, "账号或密码错误，或账号已停用")
	}
	if err = s.session(w, r, u.ID); err != nil {
		return err
	}
	return jsonResponse(w, u)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	cookie, _ := r.Cookie("oc_session")
	if _, err := s.store.Write.ExecContext(r.Context(), "DELETE FROM sessions WHERE token=?", tokenHash(cookie.Value)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "oc_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode})
	s.hub.disconnect(current(r).ID)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) profile(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Name, Password, OldPassword string }
	if err := decode(w, r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 32 {
		return fail(400, "昵称需 1–32 字")
	}
	u := current(r)
	var hash, oldHash string
	if in.Password != "" {
		if len(in.Password) < 10 || len(in.Password) > 128 {
			return fail(400, "密码需 10–128 字节")
		}
		if err := s.authGate(r); err != nil {
			return err
		}
		defer func() { <-s.authSlots }()
		if err := s.store.Read.QueryRowContext(r.Context(), "SELECT password FROM users WHERE id=?", u.ID).Scan(&oldHash); err != nil {
			return err
		}
		if !passwordMatches(oldHash, in.OldPassword) {
			return fail(400, "原密码不正确")
		}
		hash = passwordHash(in.Password)
	}
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if hash != "" {
			result, err := tx.Exec("UPDATE users SET password=? WHERE id=? AND password=?", hash, u.ID, oldHash)
			if err != nil {
				return err
			}
			n, _ := result.RowsAffected()
			if n == 0 {
				return fail(409, "密码已变更，请重新登录")
			}
			if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", u.ID); err != nil {
				return err
			}
		}
		_, err := tx.Exec("UPDATE users SET name=? WHERE id=?", in.Name, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	if hash != "" {
		s.hub.disconnect(u.ID)
		if err = s.session(w, r, u.ID); err != nil {
			return err
		}
	}
	u.Name = in.Name
	return jsonResponse(w, u)
}
func (s *Server) notify(users ...string) { s.hub.publish(users, map[string]string{"type": "refresh"}) }
func (s *Server) roomUsers(ctx context.Context, room string) []string {
	rows, err := s.store.Read.QueryContext(ctx, "SELECT user_id FROM members WHERE room_id=?", room)
	if err != nil {
		return nil
	}
	defer rows.Close()
	users := []string{}
	for rows.Next() {
		var user string
		if rows.Scan(&user) == nil {
			users = append(users, user)
		}
	}
	return users
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) error {
	user := current(r).ID
	sub, ok := s.hub.subscribe(user)
	if !ok {
		return fail(429, "实时连接已达上限（每个账号最多 4 个）")
	}
	s.presence(user)
	defer func() { s.hub.unsubscribe(user, sub); s.presence(user) }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	write := func(data []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if err := write([]byte(`{"type":"ready"}`)); err != nil {
		return nil
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return nil
		case <-r.Context().Done():
			return nil
		case <-sub.done:
			return nil
		case event := <-sub.events:
			if write(event) != nil {
				return nil
			}
		case <-ticker.C:
			if _, err := s.authenticate(r); err != nil {
				return nil
			}
			if write([]byte(`{"type":"ping"}`)) != nil {
				return nil
			}
		}
	}
}

func (s *Server) presence(user string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := s.store.Read.QueryContext(ctx, "SELECT CASE WHEN a=? THEN b ELSE a END FROM friends WHERE a=? OR b=?", user, user, user)
	if err != nil {
		return
	}
	defer rows.Close()
	users := []string{}
	for rows.Next() {
		var uid string
		if rows.Scan(&uid) == nil {
			users = append(users, uid)
		}
	}
	s.hub.publish(users, map[string]any{"type": "presence", "user_id": user, "online": s.hub.online(user)})
}
