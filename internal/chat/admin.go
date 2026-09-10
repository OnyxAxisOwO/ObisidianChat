package chat

import (
	"database/sql"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) error {
	var users, groups, messages int
	var registration string
	if err := s.store.Read.QueryRowContext(r.Context(), "SELECT (SELECT COUNT(*) FROM users),(SELECT COUNT(*) FROM rooms WHERE kind='group'),(SELECT COUNT(*) FROM messages),(SELECT value FROM settings WHERE key='registration')").Scan(&users, &groups, &messages, &registration); err != nil {
		return err
	}
	connections, online, dropped := s.hub.stats()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return jsonResponse(w, map[string]any{"users": users, "groups": groups, "messages": messages, "connections": connections, "online": online, "dropped_connections": dropped, "heap_bytes": mem.HeapAlloc, "goroutines": runtime.NumGoroutine(), "uptime_seconds": int64(time.Since(s.started).Seconds()), "registration": registration == "true"})
}
func pageOffset(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return max(0, min(n, 1000000))
}
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 96 {
		return fail(400, "搜索词过长")
	}
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT "+userCols+" FROM users WHERE username LIKE ? OR name LIKE ? ORDER BY created_at DESC,id LIMIT 50 OFFSET ?", "%"+q+"%", "%"+q+"%", pageOffset(r))
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
func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Role != nil && *in.Role != "admin" && *in.Role != "user" {
		return fail(400, "无效角色")
	}
	target, actor := r.PathValue("id"), current(r).ID
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		// Recheck authority inside the serialized transaction: another administrator may have changed it.
		if !exists(tx, "SELECT COUNT(*) FROM users WHERE id=? AND role='admin' AND disabled=0", actor) {
			return fail(403, "管理员权限已变更")
		}
		u, err := scanUser(tx.QueryRow("SELECT "+userCols+" FROM users WHERE id=?", target))
		if err == sql.ErrNoRows {
			return fail(404, "账号不存在")
		}
		if err != nil {
			return err
		}
		role, disabled := u.Role, u.Disabled
		if in.Role != nil {
			role = *in.Role
		}
		if in.Disabled != nil {
			disabled = *in.Disabled
		}
		if u.Role == "admin" && !u.Disabled && (role != "admin" || disabled) {
			var count int
			if err = tx.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0").Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return fail(409, "必须保留至少一名启用的管理员")
			}
		}
		if _, err = tx.Exec("UPDATE users SET role=?,disabled=? WHERE id=?", role, disabled, target); err != nil {
			return err
		}
		if disabled {
			if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", target); err != nil {
				return err
			}
		}
		return audit(tx, actor, "user.update role="+role+" disabled="+strconv.FormatBool(disabled), target)
	})
	if err != nil {
		return err
	}
	s.hub.disconnect(target)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) adminRooms(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT id,kind,name,owner,archived,(SELECT COUNT(*) FROM members WHERE room_id=rooms.id) FROM rooms WHERE kind='group' ORDER BY created_at DESC,id LIMIT 50 OFFSET ?", pageOffset(r))
	if err != nil {
		return err
	}
	defer rows.Close()
	rooms := []Room{}
	for rows.Next() {
		var room Room
		if err = rows.Scan(&room.ID, &room.Kind, &room.Name, &room.Owner, &room.Archived, &room.MemberCount); err != nil {
			return err
		}
		rooms = append(rooms, room)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return jsonResponse(w, rooms)
}
func (s *Server) adminUpdateRoom(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Archived bool `json:"archived"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	room := r.PathValue("id")
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE rooms SET archived=? WHERE id=? AND kind='group'", in.Archived, room)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return fail(404, "群聊不存在")
		}
		return audit(tx, current(r).ID, "group.archived="+strconv.FormatBool(in.Archived), room)
	})
	if err != nil {
		return err
	}
	s.notify(s.roomUsers(r.Context(), room)...)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT a.id,COALESCE(u.username,a.actor),a.action,a.target,a.created_at FROM audit a LEFT JOIN users u ON u.id=a.actor ORDER BY a.id DESC LIMIT 50 OFFSET ?", pageOffset(r))
	if err != nil {
		return err
	}
	defer rows.Close()
	entries := []map[string]any{}
	for rows.Next() {
		var id, at int64
		var actor, action, target string
		if err = rows.Scan(&id, &actor, &action, &target, &at); err != nil {
			return err
		}
		entries = append(entries, map[string]any{"id": id, "actor": actor, "action": action, "target": target, "created_at": at})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return jsonResponse(w, entries)
}
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Registration bool `json:"registration"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE settings SET value=? WHERE key='registration'", strconv.FormatBool(in.Registration)); err != nil {
			return err
		}
		return audit(tx, current(r).ID, "registration="+strconv.FormatBool(in.Registration), "settings")
	})
	if err != nil {
		return err
	}
	return jsonResponse(w, map[string]bool{"ok": true})
}
