import { computed, ref, onBeforeUnmount } from "vue";
import { api, APIError, errorText } from "./api";
import type { User, Room, Message, FriendData } from "./types";

export function useChat() {
  const me = ref<User | null>(null),
    loading = ref(true),
    error = ref(""),
    connected = ref(false),
    canRead = ref(true);
  const rooms = ref<Room[]>([]),
    friends = ref<FriendData>({ friends: [], requests: [] }),
    selected = ref(""),
    messages = ref<Message[]>([]),
    history = ref(false),
    browsingHistory = ref(false),
    loadingMessages = ref(false);
  const active = computed(() =>
    rooms.value.find((r) => r.id === selected.value),
  );
  let stream: EventSource | undefined,
    generation = 0,
    loadSerial = 0,
    refreshTimer: ReturnType<typeof setTimeout>,
    reconnectTimer: ReturnType<typeof setTimeout>,
    readTimer: ReturnType<typeof setTimeout>;
  let retry = 1000,
    refreshing: Promise<void> | undefined;
  function report(e: unknown) {
    error.value = errorText(e);
    if (e instanceof APIError && e.status === 401) clear();
  }
  function clear() {
    generation++;
    loadSerial++;
    stream?.close();
    clearTimeout(reconnectTimer);
    clearTimeout(refreshTimer);
    clearTimeout(readTimer);
    me.value = null;
    connected.value = false;
    rooms.value = [];
    friends.value = { friends: [], requests: [] };
    selected.value = "";
    messages.value = [];
    loadingMessages.value = false;
    browsingHistory.value = false;
  }
  async function refresh() {
    if (refreshing) {
      await refreshing;
      if (!me.value) return;
    }
    const epoch = generation;
    const pending = (async () => {
      const [r, f] = await Promise.all([
        api<Room[]>("/rooms"),
        api<FriendData>("/friends"),
      ]);
      if (epoch !== generation) return;
      rooms.value = r;
      friends.value = f;
      if (selected.value && !r.some((room) => room.id === selected.value)) {
        selected.value = "";
        messages.value = [];
      }
    })();
    refreshing = pending;
    try {
      await pending;
    } finally {
      if (refreshing === pending) refreshing = undefined;
    }
  }
  function scheduleRefresh() {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => refresh().catch(report), 180);
  }
  async function loadMessages(older = false) {
    const room = selected.value;
    if (!room) return;
    const serial = ++loadSerial;
    loadingMessages.value = true;
    const before =
      older && messages.value.length ? "?before=" + messages.value[0]!.id : "";
    try {
      const result = await api<Message[]>(
        "/rooms/" + room + "/messages" + before,
      );
      if (room !== selected.value || serial !== loadSerial) return;
      history.value = result.length === 50;
      browsingHistory.value = older;
      const latest = older
        ? [...result, ...messages.value]
        : [
            ...result,
            ...messages.value.filter((m) => m.id > (result.at(-1)?.id || 0)),
          ];
      messages.value = [...new Map(latest.map((m) => [m.id, m])).values()]
        .sort((a, b) => a.id - b.id)
        .slice(older ? 0 : -200, older ? 200 : undefined);
      if (!older) markRead();
    } catch (e) {
      if (serial === loadSerial) report(e);
    } finally {
      if (serial === loadSerial) loadingMessages.value = false;
    }
  }
  async function select(room: string) {
    if (room === selected.value) return;
    selected.value = room;
    messages.value = [];
    history.value = false;
    browsingHistory.value = false;
    await loadMessages();
  }
  function markRead() {
    clearTimeout(readTimer);
    if (document.hidden || browsingHistory.value || !canRead.value) return;
    const room = selected.value,
      message = messages.value.at(-1);
    if (!message || !active.value || message.id !== active.value.last_id)
      return;
    readTimer = setTimeout(async () => {
      if (
        document.hidden ||
        !canRead.value ||
        selected.value !== room ||
        browsingHistory.value
      )
        return;
      try {
        await api("/rooms/" + room + "/read", "POST", { id: message.id });
        const r = rooms.value.find((r) => r.id === room);
        if (r && r.last_id <= message.id) r.unread = 0;
      } catch (e) {
        report(e);
      }
    }, 400);
  }
  function receive(m: Message) {
    const room = rooms.value.find((r) => r.id === m.room_id);
    if (!room) {
      scheduleRefresh();
      return;
    }
    const isNew = m.id > room.last_id;
    if (isNew) {
      room.last_id = m.id;
      room.last_at = m.created_at;
      room.last_message = m.body;
      if (m.sender !== me.value?.id) room.unread++;
      rooms.value.sort((a, b) => b.last_at - a.last_at);
    }
    if (selected.value === m.room_id && !browsingHistory.value) {
      if (!messages.value.some((v) => v.id === m.id))
        messages.value = [...messages.value, m]
          .sort((a, b) => a.id - b.id)
          .slice(-200);
      markRead();
    }
  }
  function connect() {
    stream?.close();
    if (!me.value) return;
    const epoch = generation;
    stream = new EventSource("/api/events");
    stream.onmessage = (event) => {
      if (epoch !== generation) return;
      const data = JSON.parse(event.data);
      if (data.type === "ready") {
        connected.value = true;
        retry = 1000;
        refresh()
          .then(() => loadMessages())
          .catch(report);
      } else if (data.type === "message") receive(data.message);
      else if (data.type === "refresh") scheduleRefresh();
      else if (data.type === "presence") {
        rooms.value
          .filter((r) => r.peer_id === data.user_id)
          .forEach((r) => (r.online = data.online));
        friends.value.friends
          .filter((u) => u.id === data.user_id)
          .forEach((u) => (u.online = data.online));
      } else if (data.type === "read") {
        const room = rooms.value.find((r) => r.id === data.room_id);
        if (room && room.last_id <= data.id) room.unread = 0;
      }
    };
    stream.onerror = () => {
      connected.value = false;
      stream?.close();
      reconnectTimer = setTimeout(
        async () => {
          if (epoch !== generation) return;
          try {
            me.value = await api<User>("/me");
            connect();
          } catch (e) {
            if (e instanceof APIError && e.status === 401) report(e);
            else {
              retry = Math.min(retry * 2, 30000);
              connect();
            }
          }
        },
        retry + Math.random() * 500,
      );
      retry = Math.min(retry * 2, 30000);
    };
  }
  async function login(user: User) {
    clear();
    refreshing = undefined;
    me.value = user;
    error.value = "";
    try {
      await refresh();
      connect();
    } catch (e) {
      report(e);
    }
  }
  async function boot() {
    try {
      await login(await api<User>("/me"));
    } catch (e) {
      if (!(e instanceof APIError && e.status === 401)) report(e);
    } finally {
      loading.value = false;
    }
  }
  async function logout() {
    try {
      await api("/logout", "POST", {});
      clear();
    } catch (e) {
      report(e);
    }
  }
  async function send(body: string, clientID: string) {
    const room = selected.value;
    const m = await api<Message>("/rooms/" + room + "/messages", "POST", {
      body,
      client_id: clientID,
    });
    receive(m);
  }
  const visibility = () => {
    if (!document.hidden && me.value)
      refresh()
        .then(() => loadMessages())
        .catch(report);
  };
  document.addEventListener("visibilitychange", visibility);
  onBeforeUnmount(() => {
    clear();
    document.removeEventListener("visibilitychange", visibility);
  });
  return {
    me,
    loading,
    error,
    connected,
    canRead,
    rooms,
    friends,
    selected,
    messages,
    history,
    browsingHistory,
    loadingMessages,
    active,
    report,
    refresh,
    loadMessages,
    select,
    markRead,
    login,
    boot,
    logout,
    send,
  };
}
