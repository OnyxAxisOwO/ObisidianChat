import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
const cleanup: (() => void)[] = [];
vi.mock("vue", async () => ({
  ...(await vi.importActual("vue")),
  onBeforeUnmount: (fn: () => void) => cleanup.push(fn),
}));
import { useChat } from "./useChat";
import type { Room, User } from "./types";
const me: User = {
  id: "me",
  name: "Me",
  username: "me",
  role: "user",
  disabled: false,
  online: true,
  created_at: 0,
};
const room = (id: string): Room => ({
  id,
  kind: "group",
  name: id,
  owner: "me",
  archived: false,
  member_count: 2,
  last_message: "",
  last_at: 0,
  last_id: 0,
  unread: 0,
  peer_id: "",
  online: false,
});
const message = (id: number, roomID = "a") => ({
  id,
  room_id: roomID,
  sender: "other",
  name: "Other",
  body: "message",
  client_id: "client-" + id,
  created_at: id,
});
class Stream {
  static latest: Stream;
  onmessage?: (event: { data: string }) => void;
  onerror?: () => void;
  closed = false;
  constructor() {
    Stream.latest = this;
  }
  close() {
    this.closed = true;
  }
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }
}
let handle: (url: string) => unknown | Promise<unknown>;
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("document", {
    hidden: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
  vi.stubGlobal("EventSource", Stream);
  handle = (url) =>
    url.endsWith("/rooms")
      ? [room("a"), room("b")]
      : url.endsWith("/friends")
        ? { friends: [], requests: [] }
        : [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => ({
      ok: true,
      status: 200,
      json: async () => await handle(url),
    })),
  );
});
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
describe("chat state", () => {
  it("does not put a slow previous room response into the selected room", async () => {
    const chat = useChat();
    await chat.login(me);
    let finish: (messages: unknown[]) => void = () => {};
    handle = (url) =>
      url.includes("/a/messages")
        ? new Promise((resolve) => (finish = resolve))
        : [message(2, "b")];
    const first = chat.select("a");
    await chat.select("b");
    finish([message(1)]);
    await first;
    expect(chat.selected.value).toBe("b");
    expect(chat.messages.value.map((m) => m.room_id)).toEqual(["b"]);
  });
  it("deduplicates push and HTTP delivery and maintains the unread count", async () => {
    const chat = useChat();
    await chat.login(me);
    await chat.select("a");
    Stream.latest.emit({ type: "message", message: message(1) });
    Stream.latest.emit({ type: "message", message: message(1) });
    expect(chat.messages.value).toHaveLength(1);
    expect(chat.rooms.value[0]!.unread).toBe(1);
    await vi.advanceTimersByTimeAsync(450);
    expect(chat.rooms.value[0]!.unread).toBe(0);
  });
  it("keeps historical browsing stable while new messages arrive", async () => {
    const chat = useChat();
    await chat.login(me);
    handle = (url) =>
      url.includes("before=")
        ? Array.from({ length: 50 }, (_, i) => message(i + 1))
        : Array.from({ length: 50 }, (_, i) => message(i + 51));
    await chat.select("a");
    await chat.loadMessages(true);
    expect(chat.browsingHistory.value).toBe(true);
    expect(chat.messages.value).toHaveLength(100);
    Stream.latest.emit({ type: "message", message: message(101) });
    expect(chat.messages.value.at(-1)!.id).toBe(100);
    expect(chat.rooms.value[0]!.last_id).toBe(101);
  });
  it("does not restore messages from an outstanding request after logout", async () => {
    const chat = useChat();
    await chat.login(me);
    let finish: (value: unknown) => void = () => {};
    handle = (url) =>
      url.includes("/messages")
        ? new Promise((resolve) => (finish = resolve))
        : {};
    const loading = chat.select("a");
    await chat.logout();
    finish([message(1)]);
    await loading;
    expect(chat.me.value).toBeNull();
    expect(chat.messages.value).toEqual([]);
    expect(Stream.latest.closed).toBe(true);
  });
  it("does not mark messages read when the chat surface is hidden", async () => {
    const chat = useChat();
    await chat.login(me);
    await chat.select("a");
    chat.canRead.value = false;
    Stream.latest.emit({ type: "message", message: message(1) });
    await vi.advanceTimersByTimeAsync(450);
    expect(chat.rooms.value[0]!.unread).toBe(1);
    chat.canRead.value = true;
    chat.markRead();
    await vi.advanceTimersByTimeAsync(450);
    expect(chat.rooms.value[0]!.unread).toBe(0);
  });
});
