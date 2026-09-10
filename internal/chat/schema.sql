PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE,
 name TEXT NOT NULL, password TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user',
 disabled INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
 token TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT OR IGNORE INTO settings VALUES ('registration','true');
CREATE TABLE IF NOT EXISTS requests (
 id TEXT PRIMARY KEY, sender TEXT NOT NULL REFERENCES users(id), recipient TEXT NOT NULL REFERENCES users(id),
 created_at INTEGER NOT NULL, UNIQUE(sender, recipient), CHECK(sender <> recipient)
);
CREATE INDEX IF NOT EXISTS requests_recipient ON requests(recipient);
CREATE TABLE IF NOT EXISTS friends (
 a TEXT NOT NULL REFERENCES users(id), b TEXT NOT NULL REFERENCES users(id), PRIMARY KEY(a,b), CHECK(a < b)
);
CREATE INDEX IF NOT EXISTS friends_b ON friends(b);
CREATE TABLE IF NOT EXISTS rooms (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('direct','group')), name TEXT NOT NULL,
 owner TEXT NOT NULL REFERENCES users(id), direct_key TEXT UNIQUE, archived INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS members (
 room_id TEXT NOT NULL REFERENCES rooms(id), user_id TEXT NOT NULL REFERENCES users(id),
 last_read INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(room_id,user_id)
);
CREATE INDEX IF NOT EXISTS members_user ON members(user_id,room_id);
CREATE TABLE IF NOT EXISTS messages (
 id INTEGER PRIMARY KEY AUTOINCREMENT, room_id TEXT NOT NULL REFERENCES rooms(id),
 sender TEXT NOT NULL REFERENCES users(id), body TEXT NOT NULL, client_id TEXT NOT NULL,
 created_at INTEGER NOT NULL, UNIQUE(sender,client_id)
);
CREATE INDEX IF NOT EXISTS messages_room ON messages(room_id,id);
CREATE TABLE IF NOT EXISTS audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT, actor TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL, created_at INTEGER NOT NULL
);
