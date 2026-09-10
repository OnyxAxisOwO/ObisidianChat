package chat

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

type Room struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Archived    bool   `json:"archived"`
	MemberCount int    `json:"member_count"`
	LastMessage string `json:"last_message"`
	LastAt      int64  `json:"last_at"`
	LastID      int64  `json:"last_id"`
	Unread      int    `json:"unread"`
	PeerID      string `json:"peer_id"`
	Online      bool   `json:"online"`
}

func (s *Server) rooms(w http.ResponseWriter, r *http.Request) error {
	user := current(r).ID
	rows, err := s.store.Read.QueryContext(r.Context(), `SELECT r.id,r.kind,
 CASE WHEN r.kind='direct' THEN COALESCE((SELECT u.name FROM members n JOIN users u ON u.id=n.user_id WHERE n.room_id=r.id AND n.user_id<>?),r.name) ELSE r.name END,
 r.owner,r.archived,(SELECT COUNT(*) FROM members WHERE room_id=r.id),COALESCE(m.body,''),COALESCE(m.created_at,r.created_at),COALESCE(m.id,0),
 (SELECT COUNT(*) FROM messages WHERE room_id=r.id AND id>me.last_read AND sender<>?),
 COALESCE((SELECT user_id FROM members WHERE room_id=r.id AND user_id<>? AND r.kind='direct'),'')
 FROM members me JOIN rooms r ON r.id=me.room_id LEFT JOIN messages m ON m.id=(SELECT MAX(id) FROM messages WHERE room_id=r.id)
 WHERE me.user_id=? ORDER BY COALESCE(m.created_at,r.created_at) DESC,r.id LIMIT 200`, user, user, user, user)
	if err != nil {
		return err
	}
	defer rows.Close()
	rooms := []Room{}
	for rows.Next() {
		var room Room
		if err = rows.Scan(&room.ID, &room.Kind, &room.Name, &room.Owner, &room.Archived, &room.MemberCount, &room.LastMessage, &room.LastAt, &room.LastID, &room.Unread, &room.PeerID); err != nil {
			return err
		}
		room.Online = s.hub.online(room.PeerID)
		rooms = append(rooms, room)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return jsonResponse(w, rooms)
}
func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 48 || len(in.Members) > 255 {
		return fail(400, "群名称需 1–48 字，群人数最多 256 人")
	}
	user, room := current(r).ID, id()
	users := []string{user}
	seen := map[string]bool{user: true}
	for _, uid := range in.Members {
		if !seen[uid] {
			seen[uid] = true
			users = append(users, uid)
		}
	}
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		for _, uid := range users {
			if err := capacity(tx, uid); err != nil {
				return err
			}
			if uid != user {
				a, b := pair(uid, user)
				if !exists(tx, "SELECT COUNT(*) FROM friends f JOIN users u ON u.id=? WHERE f.a=? AND f.b=? AND u.disabled=0", uid, a, b) {
					return fail(400, "只能邀请未停用的好友")
				}
			}
		}
		if _, err := tx.Exec("INSERT INTO rooms(id,kind,name,owner,created_at) VALUES(?,'group',?,?,?)", room, in.Name, user, now()); err != nil {
			return err
		}
		for _, uid := range users {
			if _, err := tx.Exec("INSERT INTO members(room_id,user_id) VALUES(?,?)", room, uid); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.notify(users...)
	return jsonResponse(w, map[string]string{"id": room})
}
func (s *Server) members(w http.ResponseWriter, r *http.Request) error {
	var n int
	if err := s.store.Read.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM members WHERE room_id=? AND user_id=?", r.PathValue("id"), current(r).ID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fail(403, "你不在此会话中")
	}
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT u.id,u.username,u.name,u.role,u.disabled,u.created_at FROM users u JOIN members m ON m.user_id=u.id WHERE m.room_id=? ORDER BY u.name", r.PathValue("id"))
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
func groupOwner(tx *sql.Tx, room, user string) error {
	if !exists(tx, "SELECT COUNT(*) FROM rooms WHERE id=? AND kind='group' AND owner=? AND archived=0", room, user) {
		return fail(403, "需要未归档群聊的群主权限")
	}
	return membership(tx, room, user)
}
func (s *Server) addMember(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	user, room := current(r).ID, r.PathValue("id")
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if err := groupOwner(tx, room, user); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM members WHERE room_id=?", room).Scan(&count); err != nil {
			return err
		}
		if count >= 256 {
			return fail(409, "群成员已达 256 人")
		}
		a, b := pair(user, in.UserID)
		if !exists(tx, "SELECT COUNT(*) FROM friends f JOIN users u ON u.id=? WHERE f.a=? AND f.b=? AND u.disabled=0", in.UserID, a, b) {
			return fail(400, "只能邀请未停用的好友")
		}
		if err := capacity(tx, in.UserID); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO members(room_id,user_id) VALUES(?,?)", room, in.UserID)
		return err
	})
	if err != nil {
		return err
	}
	s.notify(s.roomUsers(r.Context(), room)...)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) error {
	user, room, other := current(r).ID, r.PathValue("id"), r.PathValue("user")
	users := s.roomUsers(r.Context(), room)
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if err := membership(tx, room, user); err != nil {
			return err
		}
		var owner, kind string
		if err := tx.QueryRow("SELECT owner,kind FROM rooms WHERE id=?", room).Scan(&owner, &kind); err != nil {
			return err
		}
		if kind != "group" {
			return fail(400, "请通过好友管理解除好友关系")
		}
		if other == owner {
			return fail(409, "群主需先转让群聊，或归档群聊")
		}
		if user != other && user != owner {
			return fail(403, "只有群主可以移除成员")
		}
		_, err := tx.Exec("DELETE FROM members WHERE room_id=? AND user_id=?", room, other)
		return err
	})
	if err != nil {
		return err
	}
	s.notify(users...)
	return jsonResponse(w, map[string]bool{"ok": true})
}
func (s *Server) updateRoom(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name     *string `json:"name"`
		Owner    *string `json:"owner"`
		Archived *bool   `json:"archived"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	room, user := r.PathValue("id"), current(r).ID
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if !exists(tx, "SELECT COUNT(*) FROM rooms WHERE id=? AND kind='group' AND owner=?", room, user) {
			return fail(403, "需要群主权限")
		}
		if err := membership(tx, room, user); err != nil {
			return err
		}
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if len([]rune(name)) < 1 || len([]rune(name)) > 48 {
				return fail(400, "群名称需 1–48 字")
			}
			if _, err := tx.Exec("UPDATE rooms SET name=? WHERE id=?", name, room); err != nil {
				return err
			}
		}
		if in.Owner != nil {
			if !exists(tx, "SELECT COUNT(*) FROM members m JOIN users u ON u.id=m.user_id WHERE m.room_id=? AND m.user_id=? AND u.disabled=0", room, *in.Owner) {
				return fail(400, "新群主须是未停用的群成员")
			}
			if _, err := tx.Exec("UPDATE rooms SET owner=? WHERE id=?", *in.Owner, room); err != nil {
				return err
			}
		}
		if in.Archived != nil {
			if _, err := tx.Exec("UPDATE rooms SET archived=? WHERE id=?", *in.Archived, room); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.notify(s.roomUsers(r.Context(), room)...)
	return jsonResponse(w, map[string]bool{"ok": true})
}

type Message struct {
	ID        int64  `json:"id"`
	RoomID    string `json:"room_id"`
	Sender    string `json:"sender"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) error {
	room := r.PathValue("id")
	before := int64(9223372036854775807)
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			return fail(400, "分页参数无效")
		}
		before = n
	}
	var n int
	if err := s.store.Read.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM members WHERE room_id=? AND user_id=?", room, current(r).ID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fail(403, "你不在此会话中")
	}
	rows, err := s.store.Read.QueryContext(r.Context(), "SELECT m.id,m.room_id,m.sender,u.name,m.body,m.client_id,m.created_at FROM messages m JOIN users u ON u.id=m.sender WHERE m.room_id=? AND m.id<? ORDER BY m.id DESC LIMIT 50", room, before)
	if err != nil {
		return err
	}
	defer rows.Close()
	messages := []Message{}
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.ID, &m.RoomID, &m.Sender, &m.Name, &m.Body, &m.ClientID, &m.CreatedAt); err != nil {
			return err
		}
		messages = append(messages, m)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return jsonResponse(w, messages)
}
func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Body     string `json:"body"`
		ClientID string `json:"client_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || len(in.Body) > 8192 || len(in.ClientID) < 8 || len(in.ClientID) > 64 {
		return fail(400, "消息需 1–8192 字节，且必须带有效的消息标识")
	}
	user, room := current(r), r.PathValue("id")
	m := Message{RoomID: room, Sender: user.ID, Name: user.Name, Body: in.Body, ClientID: in.ClientID, CreatedAt: now()}
	created := false
	recipients := []string{}
	err := s.store.tx(r.Context(), func(tx *sql.Tx) error {
		if err := membership(tx, room, user.ID); err != nil {
			return err
		}
		if !exists(tx, "SELECT COUNT(*) FROM rooms WHERE id=? AND archived=0", room) {
			return fail(409, "此会话已归档")
		}
		if exists(tx, "SELECT COUNT(*) FROM rooms r JOIN members n ON n.room_id=r.id JOIN users u ON u.id=n.user_id WHERE r.id=? AND r.kind='direct' AND u.disabled=1", room) {
			return fail(409, "对方账号已停用")
		}
		var oldRoom string
		err := tx.QueryRow("SELECT id,room_id,body,created_at FROM messages WHERE sender=? AND client_id=?", user.ID, in.ClientID).Scan(&m.ID, &oldRoom, &m.Body, &m.CreatedAt)
		if err == nil {
			if oldRoom != room || m.Body != in.Body {
				return fail(409, "消息标识已用于其他消息")
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		result, err := tx.Exec("INSERT INTO messages(room_id,sender,body,client_id,created_at) VALUES(?,?,?,?,?)", room, user.ID, in.Body, in.ClientID, m.CreatedAt)
		if err != nil {
			return err
		}
		m.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		created = true
		rows, err := tx.Query("SELECT user_id FROM members WHERE room_id=?", room)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var uid string
			if err = rows.Scan(&uid); err != nil {
				return err
			}
			recipients = append(recipients, uid)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	if created {
		s.hub.publish(recipients, map[string]any{"type": "message", "message": m})
	}
	return jsonResponse(w, m)
}
func (s *Server) markRead(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	room, user := r.PathValue("id"), current(r).ID
	result, err := s.store.Write.ExecContext(r.Context(), "UPDATE members SET last_read=MAX(last_read,?) WHERE room_id=? AND user_id=? AND EXISTS(SELECT 1 FROM messages WHERE id=? AND room_id=?)", in.ID, room, user, in.ID, room)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fail(400, "无效的已读位置")
	}
	s.hub.publish([]string{user}, map[string]any{"type": "read", "room_id": room, "id": in.ID})
	return jsonResponse(w, map[string]bool{"ok": true})
}
