package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	app   *Server
	http  *httptest.Server
	store *Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "chat space.db"))
	if err != nil {
		t.Fatal(err)
	}
	app := New(store, Config{SetupToken: "test-setup-token"})
	server := httptest.NewServer(app.Handler(nil))
	f := &fixture{app, server, store}
	t.Cleanup(func() { app.Shutdown(); server.Close(); store.Close() })
	return f
}
func (f *fixture) request(t *testing.T, cookie *http.Cookie, method, path string, body any, status int) []byte {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(method, f.http.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s %s = %d want %d: %s", method, path, res.StatusCode, status, data)
	}
	return data
}
func (f *fixture) account(t *testing.T, name string, setup bool) (User, *http.Cookie) {
	t.Helper()
	path := "/api/register"
	if setup {
		path = "/api/setup"
	}
	payload, _ := json.Marshal(map[string]string{"username": name, "name": name, "password": "test-password-123", "token": "test-setup-token"})
	res, err := http.Post(f.http.URL+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("account %s: %d %s", name, res.StatusCode, data)
	}
	var u User
	if err = json.Unmarshal(data, &u); err != nil {
		t.Fatal(err)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("missing secure session attributes")
	}
	return u, cookies[0]
}
func (f *fixture) befriend(t *testing.T, a, b User, ca, cb *http.Cookie) string {
	t.Helper()
	f.request(t, ca, "POST", "/api/friends/requests", map[string]string{"user_id": b.ID}, 200)
	var data struct {
		Requests []friendRequest `json:"requests"`
	}
	json.Unmarshal(f.request(t, cb, "GET", "/api/friends", nil, 200), &data)
	if len(data.Requests) != 1 {
		t.Fatalf("requests: %+v", data)
	}
	var result map[string]string
	json.Unmarshal(f.request(t, cb, "POST", "/api/friends/requests/"+data.Requests[0].ID, map[string]string{"action": "accept"}, 200), &result)
	return result["room_id"]
}
func TestChatLifecycle(t *testing.T) {
	f := newFixture(t)
	a, ca := f.account(t, "admin", true)
	b, cb := f.account(t, "alice", false)
	c, cc := f.account(t, "bob", false)
	f.request(t, nil, "GET", "/api/rooms", nil, 401)
	f.request(t, ca, "POST", "/api/setup", map[string]string{"username": "other", "name": "Other", "password": "test-password-123", "token": "test-setup-token"}, 409)
	room := f.befriend(t, a, b, ca, cb)
	body := map[string]string{"body": "你好\nmessage <literal>", "client_id": "message-0001"}
	var first, again Message
	json.Unmarshal(f.request(t, ca, "POST", "/api/rooms/"+room+"/messages", body, 200), &first)
	json.Unmarshal(f.request(t, ca, "POST", "/api/rooms/"+room+"/messages", body, 200), &again)
	if first.ID == 0 || first.ID != again.ID {
		t.Fatal("idempotent retry changed message")
	}
	var messages []Message
	json.Unmarshal(f.request(t, cb, "GET", "/api/rooms/"+room+"/messages", nil, 200), &messages)
	if len(messages) != 1 || messages[0].Body != body["body"] {
		t.Fatalf("messages %+v", messages)
	}
	var rooms []Room
	json.Unmarshal(f.request(t, cb, "GET", "/api/rooms", nil, 200), &rooms)
	if len(rooms) != 1 || rooms[0].Unread != 1 || rooms[0].Name != a.Name {
		t.Fatalf("rooms %+v", rooms)
	}
	f.request(t, cb, "POST", "/api/rooms/"+room+"/read", map[string]int64{"id": first.ID}, 200)
	json.Unmarshal(f.request(t, cb, "GET", "/api/rooms", nil, 200), &rooms)
	if rooms[0].Unread != 0 {
		t.Fatal("read cursor not persisted")
	}
	f.request(t, cc, "GET", "/api/rooms/"+room+"/messages", nil, 403)
	f.request(t, cc, "POST", "/api/rooms/"+room+"/messages", body, 403)
	f.request(t, cc, "GET", "/api/rooms/"+room+"/members", nil, 403)
	f.request(t, ca, "POST", "/api/rooms", map[string]any{"name": "Invalid", "members": []string{c.ID}}, 400)
	var group map[string]string
	json.Unmarshal(f.request(t, ca, "POST", "/api/rooms", map[string]any{"name": "开发组", "members": []string{b.ID, b.ID}}, 200), &group)
	g := group["id"]
	f.request(t, cb, "PATCH", "/api/rooms/"+g, map[string]string{"name": "改名"}, 403)
	f.request(t, ca, "DELETE", "/api/rooms/"+g+"/members/"+a.ID, nil, 409)
	f.request(t, ca, "PATCH", "/api/rooms/"+g, map[string]string{"owner": b.ID}, 200)
	f.request(t, cb, "DELETE", "/api/rooms/"+g+"/members/"+a.ID, nil, 200)
	f.request(t, ca, "GET", "/api/rooms/"+g+"/messages", nil, 403)
	f.request(t, cb, "PATCH", "/api/rooms/"+g, map[string]bool{"archived": true}, 200)
	f.request(t, cb, "POST", "/api/rooms/"+g+"/messages", body, 409)
	f.request(t, ca, "DELETE", "/api/friends/"+b.ID, nil, 200)
	f.request(t, ca, "POST", "/api/rooms/"+room+"/messages", body, 409)
	reopened := f.befriend(t, a, b, ca, cb)
	if reopened != room {
		t.Fatal("friendship recovery lost original history")
	}
}
func TestAdminRulesAndSessions(t *testing.T) {
	f := newFixture(t)
	a, ca := f.account(t, "admin", true)
	b, cb := f.account(t, "alice", false)
	routes := []struct {
		method, path string
		body         any
	}{{"GET", "/api/admin/stats", nil}, {"GET", "/api/admin/users", nil}, {"PATCH", "/api/admin/users/" + a.ID, map[string]bool{"disabled": true}}, {"GET", "/api/admin/rooms", nil}, {"PATCH", "/api/admin/rooms/no-room", map[string]bool{"archived": true}}, {"GET", "/api/admin/audit", nil}, {"PATCH", "/api/admin/settings", map[string]bool{"registration": false}}}
	for _, route := range routes {
		f.request(t, cb, route.method, route.path, route.body, 403)
		f.request(t, nil, route.method, route.path, route.body, 401)
	}
	f.request(t, ca, "PATCH", "/api/admin/users/"+a.ID, map[string]bool{"disabled": true}, 409)
	f.request(t, ca, "PATCH", "/api/admin/users/"+a.ID, map[string]string{"role": "user"}, 409)
	f.request(t, ca, "PATCH", "/api/admin/users/"+b.ID, map[string]bool{"disabled": true}, 200)
	f.request(t, cb, "GET", "/api/me", nil, 401)
	f.request(t, nil, "POST", "/api/login", map[string]string{"username": "alice", "password": "test-password-123"}, 401)
	f.request(t, ca, "PATCH", "/api/admin/settings", map[string]bool{"registration": false}, 200)
	f.request(t, nil, "POST", "/api/register", map[string]string{"username": "third", "name": "Third", "password": "test-password-123"}, 403)
	f.request(t, ca, "PATCH", "/api/me", map[string]string{"name": "New admin", "password": "new-password-456", "oldPassword": "bad"}, 400)
	f.request(t, ca, "PATCH", "/api/me", map[string]string{"name": "New admin", "password": "new-password-456", "oldPassword": "test-password-123"}, 200)
	f.request(t, ca, "GET", "/api/me", nil, 401)
}
func TestConcurrentLastAdministrator(t *testing.T) {
	f := newFixture(t)
	a, ca := f.account(t, "admin", true)
	b, cb := f.account(t, "other", false)
	f.request(t, ca, "PATCH", "/api/admin/users/"+b.ID, map[string]string{"role": "admin"}, 200)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, item := range []struct {
		user   User
		cookie *http.Cookie
	}{{a, ca}, {b, cb}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("PATCH", f.http.URL+"/api/admin/users/"+item.user.ID, strings.NewReader(`{"role":"user"}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(item.cookie)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			statuses <- res.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status != 409 {
			t.Fatalf("unexpected status %d", status)
		}
	}
	if success != 1 {
		t.Fatalf("successful demotions %d", success)
	}
	var admins int
	f.store.Read.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0").Scan(&admins)
	if admins != 1 {
		t.Fatal("lost last admin")
	}
}
func TestStreamDeliveryAndBackpressure(t *testing.T) {
	f := newFixture(t)
	a, ca := f.account(t, "admin", true)
	b, cb := f.account(t, "alice", false)
	room := f.befriend(t, a, b, ca, cb)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.http.URL+"/api/events", nil)
	req.AddCookie(cb)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, "ready") {
		t.Fatalf("initial event: %s %v", line, err)
	}
	reader.ReadString('\n')
	f.request(t, ca, "POST", "/api/rooms/"+room+"/messages", map[string]string{"body": "live", "client_id": "live-message"}, 200)
	line, err = reader.ReadString('\n')
	if err != nil || !strings.Contains(line, `"body":"live"`) {
		t.Fatalf("live event: %s %v", line, err)
	}
	h := newHub()
	slow, _ := h.subscribe("slow")
	fast, _ := h.subscribe("fast")
	for i := 0; i < 40; i++ {
		h.publish([]string{"slow", "fast"}, map[string]int{"n": i})
		<-fast.events
	}
	select {
	case <-slow.done:
	default:
		t.Fatal("slow client was not disconnected")
	}
	connections, _, dropped := h.stats()
	if connections != 1 || dropped != 1 {
		t.Fatal("backpressure accounting")
	}
	h.unsubscribe("slow", slow)
	h.unsubscribe("fast", fast)
}
func TestConcurrentMessagesAndPagination(t *testing.T) {
	f := newFixture(t)
	a, ca := f.account(t, "admin", true)
	b, cb := f.account(t, "alice", false)
	room := f.befriend(t, a, b, ca, cb)
	// Seed enough persisted history to exercise cursor boundaries independently of write rate limits.
	for i := 0; i < 80; i++ {
		if _, err := f.store.Write.Exec("INSERT INTO messages(room_id,sender,body,client_id,created_at) VALUES(?,?,?,?,?)", room, a.ID, fmt.Sprintf("msg %d", i), fmt.Sprintf("seed-%d", i), now()); err != nil {
			t.Fatal(err)
		}
	}
	var latest, older []Message
	json.Unmarshal(f.request(t, cb, "GET", "/api/rooms/"+room+"/messages", nil, 200), &latest)
	if len(latest) != 50 {
		t.Fatal("page size")
	}
	json.Unmarshal(f.request(t, cb, "GET", fmt.Sprintf("/api/rooms/%s/messages?before=%d", room, latest[0].ID), nil, 200), &older)
	if len(older) != 30 || older[len(older)-1].ID >= latest[0].ID {
		t.Fatal("history cursor overlap")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f.request(t, ca, "POST", "/api/rooms/"+room+"/messages", map[string]string{"body": "concurrent", "client_id": fmt.Sprintf("parallel-%02d", i)}, 200)
		}(i)
	}
	wg.Wait()
	var count int
	f.store.Read.QueryRow("SELECT COUNT(*) FROM messages WHERE room_id=?", room).Scan(&count)
	if count != 92 {
		t.Fatalf("persisted %d messages", count)
	}
}
func TestRequestValidation(t *testing.T) {
	f := newFixture(t)
	_, ca := f.account(t, "admin", true)
	req, _ := http.NewRequest("POST", f.http.URL+"/api/logout", strings.NewReader(`{}`))
	req.AddCookie(ca)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://untrusted.invalid")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("origin policy")
	}
	f.request(t, ca, "PATCH", "/api/me", map[string]string{"name": "ok", "extra": "unknown"}, 400)
	f.request(t, ca, "GET", "/api/users?q=ad", nil, 200)
	f.request(t, ca, "GET", "/api/users?q=a_", nil, 200)
	f.request(t, ca, "POST", "/api/rooms", map[string]string{"name": strings.Repeat("x", 17000)}, 400)
}
func TestStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write.Exec("INSERT INTO users(id,username,name,password,role,created_at) VALUES('one','one','One','hash','admin',1)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err = s.Read.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reopen %d %v", count, err)
	}
}
