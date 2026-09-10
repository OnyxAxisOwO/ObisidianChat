export interface User {
  id: string;
  username: string;
  name: string;
  role: "admin" | "user";
  disabled: boolean;
  online: boolean;
  created_at: number;
}
export interface Room {
  id: string;
  kind: "direct" | "group";
  name: string;
  owner: string;
  archived: boolean;
  member_count: number;
  last_message: string;
  last_at: number;
  last_id: number;
  unread: number;
  peer_id: string;
  online: boolean;
}
export interface Message {
  id: number;
  room_id: string;
  sender: string;
  name: string;
  body: string;
  client_id: string;
  created_at: number;
}
export interface FriendRequest {
  id: string;
  sender: string;
  recipient: string;
  name: string;
  username: string;
  created_at: number;
}
export interface FriendData {
  friends: User[];
  requests: FriendRequest[];
}
export interface Stats {
  users: number;
  groups: number;
  messages: number;
  connections: number;
  online: number;
  dropped_connections: number;
  heap_bytes: number;
  goroutines: number;
  uptime_seconds: number;
  registration: boolean;
}
