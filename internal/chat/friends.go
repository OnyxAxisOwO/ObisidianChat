package chat

import (
	"database/sql"
	"net/http"
	"strings"
)

func (s *Server) searchUsers(w http.ResponseWriter, r *http.Request) error {
	if !s.limits.allow("search:"+current(r).ID, 20, 2) {
		return fail(429, "搜索太频繁")
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 96 {
		return jsonResponse(w, []User{})
	}
	q = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q)
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT "+userCols+" FROM users WHERE disabled=0 AND id<>? AND (username LIKE ? ESCAPE '\\' OR name LIKE ? ESCAPE '\\') ORDER BY username LIMIT 30", current(r).ID, q+"%", q+"%")
	if err != nil {
		return err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return err
		}
		u.Online = s.hub.online(u.ID)
		users = append(users, u)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return jsonResponse(w, users)
}

type friendRequest struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) friends(w http.ResponseWriter, r *http.Request) error {
	user := current(r).ID
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT u.id,u.username,u.name,u.role,u.disabled,u.created_at FROM friends f JOIN users u ON u.id=CASE WHEN f.a=? THEN f.b ELSE f.a END WHERE f.a=? OR f.b=? ORDER BY u.name", user, user, user)
	if err != nil {
		return err
	}
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return err
		}
		u.Online = s.hub.online(u.ID)
		users = append(users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = s.store.Read.QueryContext(r.Context(), "SELECT f.id,f.sender,f.recipient,u.name,u.username,f.created_at FROM requests f JOIN users u ON u.id=CASE WHEN f.sender=? THEN f.recipient ELSE f.sender END WHERE f.sender=? OR f.recipient=? ORDER BY f.created_at DESC", user, user, user)
	if err != nil {
		return err
	}
	defer rows.Close()
	requests := []friendRequest{}
	for rows.Next() {
		var f friendRequest
		if err = rows.Scan(&f.ID, &f.Sender, &f.Recipient, &f.Name, &f.Username, &f.CreatedAt); err != nil {
			return err
		}
		requests = append(requests, f)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return jsonResponse(w, map[string]any{"friends": users, "requests": requests})
}
func (s *Server) requestFriend(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	user := current(r).ID
	if user == in.UserID {
		return fail(400, "不能添加自己")
	}
	a, b := pair(user, in.UserID)
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if !exists(tx, "SELECT COUNT(*) FROM users WHERE id=? AND disabled=0", in.UserID) {
			return fail(404, "账号不存在或已停用")
		}
		if exists(tx, "SELECT COUNT(*) FROM friends WHERE a=? AND b=?", a, b) {
			return fail(409, "已是好友")
		}
		if exists(tx, "SELECT COUNT(*) FROM requests WHERE sender=? AND recipient=?", in.UserID, user) {
			return fail(409, "对方已申请添加你，请在好友申请中处理")
		}
		for _, uid := range []string{user, in.UserID} {
			var n int
			if err := tx.QueryRow("SELECT COUNT(*) FROM requests WHERE sender=? OR recipient=?", uid, uid).Scan(&n); err != nil {
				return err
			}
			if n >= 100 {
				return fail(409, "待处理申请已达上限")
			}
		}
		_, err := tx.Exec("INSERT INTO requests VALUES(?,?,?,?)", id(), user, in.UserID, now())
		return err
	})
	if err != nil {
		return err
	}
	s.notify(user, in.UserID)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) resolveRequest(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Action string `json:"action"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Action != "accept" && in.Action != "reject" && in.Action != "cancel" {
		return fail(400, "无效操作")
	}
	user := current(r).ID
	var sender, recipient, room string
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT sender,recipient FROM requests WHERE id=?", r.PathValue("id")).Scan(&sender, &recipient); err == sql.ErrNoRows {
			return fail(404, "申请已被处理")
		} else if err != nil {
			return err
		}
		if (in.Action == "cancel" && user != sender) || (in.Action != "cancel" && user != recipient) {
			return fail(403, "无法处理此申请")
		}
		if in.Action == "accept" {
			if !exists(tx, "SELECT COUNT(*) FROM users WHERE id=? AND disabled=0", sender) {
				return fail(409, "对方账号已停用")
			}
			a, b := pair(sender, recipient)
			key := a + ":" + b
			err := tx.QueryRow("SELECT id FROM rooms WHERE direct_key=?", key).Scan(&room)
			if err == sql.ErrNoRows {
				for _, uid := range []string{sender, recipient} {
					if err = capacity(tx, uid); err != nil {
						return err
					}
				}
				room = id()
				if _, err = tx.Exec("INSERT INTO rooms(id,kind,name,owner,direct_key,created_at) VALUES(?,'direct','',?,?,?)", room, sender, key, now()); err != nil {
					return err
				}
				for _, uid := range []string{sender, recipient} {
					if _, err = tx.Exec("INSERT INTO members(room_id,user_id) VALUES(?,?)", room, uid); err != nil {
						return err
					}
				}
			} else if err != nil {
				return err
			} else {
				if _, err = tx.Exec("UPDATE rooms SET archived=0 WHERE id=?", room); err != nil {
					return err
				}
			}
			if _, err = tx.Exec("INSERT OR IGNORE INTO friends VALUES(?,?)", a, b); err != nil {
				return err
			}
		}
		_, err := tx.Exec("DELETE FROM requests WHERE id=?", r.PathValue("id"))
		return err
	})
	if err != nil {
		return err
	}
	s.notify(sender, recipient)
	return jsonResponse(w, map[string]string{"room_id": room})
}
func (s *Server) removeFriend(w http.ResponseWriter, r *http.Request) error {
	user, other := current(r).ID, r.PathValue("id")
	a, b := pair(user, other)
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM friends WHERE a=? AND b=?", a, b); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE rooms SET archived=1 WHERE direct_key=?", a+":"+b)
		return err
	})
	if err != nil {
		return err
	}
	s.notify(user, other)
	return jsonResponse(w, map[string]bool{"ok": true})
}
