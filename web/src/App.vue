<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  nextTick,
  onMounted,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
  watchEffect,
} from "vue";
import {
  MessageSquare,
  Users,
  Search,
  Plus,
  Settings,
  Moon,
  Sun,
  ArrowUp,
  Shield,
  ChevronLeft,
  MoreHorizontal,
  UserPlus,
  X,
  RefreshCw,
} from "lucide-vue-next";
import AuthForm from "./AuthForm.vue";
import FriendPanel from "./FriendPanel.vue";
import RoomPanel from "./RoomPanel.vue";
import AccountPanel from "./AccountPanel.vue";
import { useChat } from "./useChat";
import { time, errorText, newClientID } from "./api";
import type { Message } from "./types";
const AdminView = defineAsyncComponent(() => import("./AdminView.vue"));
const {
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
} = useChat();
const draft = ref(""),
  filter = ref("all"),
  query = ref(""),
  panel = ref(""),
  admin = ref(false),
  sending = ref(false),
  feed = ref<HTMLElement>(),
  composer = ref<HTMLTextAreaElement>(),
  olderLoading = ref(false),
  mobileChat = ref(false),
  sendError = ref("");
const renderedPanel = ref("");
const enteringMessages = shallowRef(new Set<number>());
let panelTrigger: HTMLElement | null = null;
watch(panel, (value, previous) => {
  if (value) {
    if (!previous && document.activeElement instanceof HTMLElement)
      panelTrigger = document.activeElement;
    // Keep the outgoing contents mounted until the column finishes closing.
    renderedPanel.value = value;
  }
});
function focusPanel(element: Element) {
  element
    .querySelector<HTMLElement>("button, input, select")
    ?.focus({ preventScroll: true });
}
function restorePanelFocus() {
  if (!panel.value && panelTrigger?.isConnected)
    panelTrigger.focus({ preventScroll: true });
}
watch(
  messages,
  (value, previous) => {
    // History loads animate as one conversation rather than as individual stored messages.
    if (loadingMessages.value || olderLoading.value || browsingHistory.value) {
      enteringMessages.value = new Set();
      return;
    }
    const previousIDs = new Set(previous.map((message) => message.id));
    enteringMessages.value = new Set(
      value
        .filter((message) => !previousIDs.has(message.id))
        .map((message) => message.id),
    );
  },
  { flush: "sync" },
);
const theme = ref(
  localStorage.getItem("oc-theme") ||
    (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"),
);
const mobileQuery = matchMedia("(max-width:640px)"),
  mobile = ref(mobileQuery.matches);
const resize = () => (mobile.value = mobileQuery.matches);
mobileQuery.addEventListener("change", resize);
onBeforeUnmount(() => mobileQuery.removeEventListener("change", resize));
watchEffect(() => {
  canRead.value =
    !admin.value && (!mobile.value || (mobileChat.value && !panel.value));
});
watch(canRead, (value) => {
  if (value) markRead();
});
watch(
  theme,
  (value) => {
    document.documentElement.dataset.theme = value;
    localStorage.setItem("oc-theme", value);
  },
  { immediate: true },
);
const visible = computed(() =>
  rooms.value.filter(
    (r) =>
      (filter.value === "all" ||
        (filter.value === "friends"
          ? r.kind === "direct"
          : r.kind === "group")) &&
      r.name.toLowerCase().includes(query.value.toLowerCase()),
  ),
);
const incoming = computed(
  () =>
    friends.value.requests.filter((r) => r.recipient === me.value?.id).length,
);
const unread = computed(() => rooms.value.reduce((n, r) => n + r.unread, 0));
watch(
  unread,
  (value) => (document.title = (value ? `(${value}) ` : "") + "Obsidian Chat"),
);
const drafts = new Map<string, string>();
const pending = new Map<string, { body: string; id: string }>();
watch(selected, (value, old) => {
  if (old) drafts.set(old, draft.value);
  draft.value = drafts.get(value) || "";
  sendError.value = "";
  if (!value) panel.value = "";
});
watch(me, (value) => {
  if (!value) {
    panel.value = "";
    admin.value = false;
    draft.value = "";
    drafts.clear();
    pending.clear();
  }
});
watch(
  () => messages.value.at(-1)?.id,
  async () => {
    const nearBottom =
      !feed.value ||
      feed.value.scrollHeight - feed.value.scrollTop - feed.value.clientHeight <
        180;
    await nextTick();
    if (nearBottom && !olderLoading.value) bottom();
  },
);
function bottom() {
  if (feed.value) feed.value.scrollTop = feed.value.scrollHeight;
}
async function openRoom(id: string) {
  mobileChat.value = true;
  panel.value = "";
  admin.value = false;
  await select(id);
  await nextTick();
  bottom();
  composer.value?.focus();
}
async function changedRoom(id: string) {
  try {
    await refresh();
    await openRoom(id);
  } catch (e) {
    report(e);
  }
}
async function changed() {
  try {
    await refresh();
  } catch (e) {
    report(e);
  }
}
async function submit() {
  if (
    sending.value ||
    !draft.value.trim() ||
    !active.value ||
    active.value.archived
  )
    return;
  const room = selected.value,
    body = draft.value.trim();
  let attempt = pending.get(room);
  if (!attempt || attempt.body !== body) {
    attempt = { body, id: newClientID() };
    pending.set(room, attempt);
  }
  sending.value = true;
  sendError.value = "";
  try {
    await send(body, attempt.id);
    pending.delete(room);
    drafts.delete(room);
    if (selected.value === room && draft.value.trim() === body)
      draft.value = "";
    await nextTick();
    bottom();
    composer.value?.focus();
  } catch (e) {
    sendError.value = errorText(e);
  } finally {
    sending.value = false;
  }
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    submit();
  }
}
async function older() {
  if (!feed.value) return;
  olderLoading.value = true;
  const height = feed.value.scrollHeight;
  await loadMessages(true);
  await nextTick();
  if (feed.value) feed.value.scrollTop += feed.value.scrollHeight - height;
  olderLoading.value = false;
}
const day = (at: number) =>
  new Date(at).toLocaleDateString("zh-CN", { month: "long", day: "numeric" });
const messageGroupWindow = 5 * 60 * 1000;
function sameMessageGroup(message: Message, adjacent?: Message) {
  return (
    !!adjacent &&
    message.sender === adjacent.sender &&
    day(message.created_at) === day(adjacent.created_at) &&
    Math.abs(message.created_at - adjacent.created_at) <= messageGroupWindow
  );
}
onMounted(boot);
</script>
<template>
  <div class="workspace">
    <header class="topbar">
      <a class="brand" href="/" @click.prevent="admin = false"
        ><span class="brand-mark"><MessageSquare :size="17" /></span>Obsidian
        <b>Chat</b></a
      ><span class="spacer" /><span v-if="me" class="connection"
        ><i :class="{ connected }" />{{ connected ? "已连接" : "重连中" }}</span
      ><button
        v-if="me?.role === 'admin'"
        class="icon-btn"
        :class="{ chosen: admin }"
        title="管理后台"
        aria-label="管理后台"
        @click="
          admin = !admin;
          panel = '';
        "
      >
        <Shield :size="18" /></button
      ><button
        class="icon-btn"
        title="切换主题"
        aria-label="切换主题"
        @click="theme = theme === 'dark' ? 'light' : 'dark'"
      >
        <Sun v-if="theme === 'dark'" :size="18" /><Moon
          v-else
          :size="18"
        /></button
      ><button
        v-if="me"
        class="avatar small"
        title="账号设置"
        aria-label="账号设置"
        @click="
          admin = false;
          panel = panel === 'account' ? '' : 'account';
        "
      >
        {{ me.name.slice(0, 1) }}</button
      ><button v-else class="mobile-only" @click="mobileChat = true">
        登录
      </button>
    </header>
    <div v-if="error" class="error-banner" role="alert">
      <span>{{ error }}</span
      ><button class="icon-btn" aria-label="关闭提示" @click="error = ''">
        <X :size="16" />
      </button>
    </div>
    <Transition name="workspace-view" mode="out-in">
      <AdminView
        v-if="admin && me?.role === 'admin'"
        :me="me"
        @close="admin = false"
        @changed="changed"
        @session-changed="boot"
      />
      <main
        v-else
        class="layout"
        :class="{ 'has-room': mobileChat, 'has-panel': !!panel }"
      >
        <aside class="contacts panel" :inert="mobile && !!panel">
          <div class="section-head">
            <h1>
              联系人
              <span v-if="rooms.length" class="count">{{ rooms.length }}</span>
            </h1>
            <button
              class="icon-btn"
              title="添加好友"
              aria-label="添加好友"
              :disabled="!me"
              @click="panel = panel === 'friends' ? '' : 'friends'"
            >
              <Plus :size="19" />
            </button>
          </div>
          <label class="search"
            ><Search :size="16" /><input
              v-model="query"
              placeholder="搜索联系人或群聊"
              aria-label="搜索联系人或群聊"
          /></label>
          <div
            class="tabs"
            aria-label="联系人筛选"
            :style="{
              '--tab-index': ['all', 'friends', 'groups'].indexOf(filter),
            }"
          >
            <button
              :class="{ active: filter === 'all' }"
              :aria-pressed="filter === 'all'"
              @click="filter = 'all'"
            >
              全部</button
            ><button
              :class="{ active: filter === 'friends' }"
              :aria-pressed="filter === 'friends'"
              @click="filter = 'friends'"
            >
              好友</button
            ><button
              :class="{ active: filter === 'groups' }"
              :aria-pressed="filter === 'groups'"
              @click="filter = 'groups'"
            >
              群聊
            </button>
          </div>
          <button
            v-if="incoming"
            class="request-notice"
            @click="panel = 'friends'"
          >
            <UserPlus :size="16" /> 好友申请
            <span class="badge">{{ incoming }}</span>
          </button>
          <div :key="filter" class="contact-list">
            <button
              v-for="room in visible"
              :key="room.id"
              class="contact-row"
              :class="{ selected: room.id === selected }"
              @click="openRoom(room.id)"
            >
              <span class="avatar" :class="{ group: room.kind === 'group' }"
                ><Users v-if="room.kind === 'group'" :size="19" /><template
                  v-else
                  >{{ room.name.slice(0, 1) }}</template
                ><i
                  v-if="room.kind === 'direct' && room.online"
                  class="online-dot" /></span
              ><span class="contact-text"
                ><span class="contact-title"
                  ><strong>{{ room.name }}</strong
                  ><time>{{ time(room.last_at) }}</time></span
                ><span class="contact-preview"
                  ><span
                    >{{ room.archived ? "[已归档] " : ""
                    }}{{ room.last_message || "暂无消息" }}</span
                  ><span v-if="room.unread" class="badge">{{
                    room.unread > 99 ? "99+" : room.unread
                  }}</span></span
                ></span
              >
            </button>
            <div v-if="!visible.length" class="empty">
              <Users :size="25" />
              <p>
                {{
                  loading
                    ? "加载中…"
                    : !me
                      ? "登录后查看联系人"
                      : query
                        ? "未找到联系人"
                        : "暂无联系人"
                }}
              </p>
              <div v-if="me && !query" class="stack">
                <button @click="panel = 'friends'">添加好友</button
                ><button @click="panel = 'group'">创建群聊</button>
              </div>
              <button
                v-if="!me"
                class="mobile-only primary"
                @click="mobileChat = true"
              >
                登录
              </button>
            </div>
          </div>
          <div v-if="me" class="contact-actions">
            <button @click="panel = 'friends'">
              <UserPlus :size="16" /> 好友管理</button
            ><button @click="panel = 'group'"><Users :size="16" /> 建群</button>
          </div>
          <button
            class="contact-footer"
            :disabled="!me"
            @click="panel = panel === 'account' ? '' : 'account'"
          >
            <span class="avatar small">{{ me?.name.slice(0, 1) || "O" }}</span>
            <div>
              <strong>{{ me?.name || "未登录" }}</strong>
              <p>{{ me ? "@" + me.username : "" }}</p>
            </div>
            <span class="spacer" /><Settings :size="17" />
          </button>
        </aside>
        <section class="chat panel" :inert="mobile && !!panel">
          <div class="chat-head">
            <button
              class="mobile-only icon-btn"
              aria-label="返回联系人"
              @click="mobileChat = false"
            >
              <ChevronLeft :size="20" />
            </button>
            <div v-if="active" class="avatar">
              <Users v-if="active.kind === 'group'" :size="20" /><template
                v-else
                >{{ active.name.slice(0, 1) }}</template
              >
            </div>
            <div>
              <h2>{{ active?.name || "聊天" }}</h2>
              <p v-if="active">
                {{
                  active.archived
                    ? "已归档"
                    : active.kind === "group"
                      ? active.member_count + " 位成员"
                      : active.online
                        ? "在线"
                        : "离线"
                }}
              </p>
            </div>
            <span class="spacer" /><button
              v-if="active"
              class="icon-btn"
              aria-label="会话详情"
              title="会话详情"
              @click="panel = panel === 'room' ? '' : 'room'"
            >
              <MoreHorizontal :size="21" />
            </button>
          </div>
          <div v-if="loading" class="empty"><p>加载中…</p></div>
          <AuthForm v-else-if="!me" @login="login" />
          <div v-else-if="!active" class="empty">
            <MessageSquare :size="34" />
            <p>选择联系人或群聊</p>
          </div>
          <template v-else
            ><div
              ref="feed"
              :key="selected"
              class="messages"
              role="log"
              aria-label="聊天消息"
            >
              <div class="history-control">
                <button
                  v-if="history"
                  :disabled="loadingMessages"
                  @click="older"
                >
                  {{ olderLoading ? "加载中…" : "查看更早消息" }}</button
                ><button
                  v-if="browsingHistory"
                  @click="
                    messages = [];
                    loadMessages();
                  "
                >
                  返回最新消息
                </button>
              </div>
              <p
                v-if="loadingMessages && !messages.length"
                class="muted-note centered"
              >
                加载中…
              </p>
              <p v-else-if="!messages.length" class="muted-note centered">
                暂无消息
              </p>
              <template v-for="(message, i) in messages" :key="message.id"
                ><div
                  v-if="
                    i === 0 ||
                    day(message.created_at) !== day(messages[i - 1]!.created_at)
                  "
                  class="date-divider"
                >
                  {{ day(message.created_at) }}
                </div>
                <div
                  class="message-row"
                  :class="{
                    mine: message.sender === me.id,
                    'group-start': !sameMessageGroup(
                      message,
                      messages[i - 1],
                    ),
                    'group-end': !sameMessageGroup(message, messages[i + 1]),
                    'message-enter': enteringMessages.has(message.id),
                  }"
                >
                  <span
                    v-if="message.sender !== me.id"
                    class="message-avatar-slot"
                  >
                    <span
                      v-if="!sameMessageGroup(message, messages[i + 1])"
                      class="avatar small"
                      >{{ message.name.slice(0, 1) }}</span
                    >
                  </span>
                  <div class="message-content">
                    <span
                      v-if="!sameMessageGroup(message, messages[i - 1])"
                      class="message-meta"
                      >{{ message.sender === me.id ? "我" : message.name }}
                      <time>{{ time(message.created_at) }}</time></span
                    >
                    <div class="bubble">{{ message.body }}</div>
                  </div>
                </div></template
              >
            </div>
            <p v-if="sendError" class="send-error error" role="alert">
              {{ sendError }} · 内容已保留，可重试发送
            </p>
            <form class="composer" @submit.prevent="submit">
              <textarea
                ref="composer"
                v-model="draft"
                :disabled="active.archived || sending"
                :placeholder="active.archived ? '此会话已归档' : '输入消息…'"
                aria-label="消息"
                rows="2"
                maxlength="8192"
                @keydown="keydown"
              />
              <div class="composer-bottom">
                <span>Enter 发送 · Shift + Enter 换行</span
                ><span class="spacer" /><span v-if="draft.length">{{
                  draft.length
                }}</span
                ><button
                  class="primary send"
                  :disabled="!draft.trim() || active.archived || sending"
                  aria-label="发送消息"
                >
                  <RefreshCw v-if="sending" :size="17" class="spin" /><ArrowUp
                    v-else
                    :size="20"
                  />
                </button>
              </div></form
          ></template>
        </section>
        <Transition
          name="panel-slide"
          @after-enter="focusPanel"
          @after-leave="restorePanelFocus"
        >
          <div
            v-if="me && panel"
            class="panel-slot"
            :inert="!panel"
            @keydown.esc.stop="panel = ''"
          >
            <Transition name="panel-content" mode="out-in">
              <FriendPanel
                v-if="renderedPanel === 'friends' || renderedPanel === 'group'"
                :key="renderedPanel"
                :me="me"
                :data="friends"
                :mode="renderedPanel"
                @close="panel = ''"
                @changed="changed"
                @room="changedRoom"
              />
              <RoomPanel
                v-else-if="renderedPanel === 'room' && active"
                :key="active.id"
                :room="active"
                :me="me"
                :friends="friends.friends"
                @close="panel = ''"
                @changed="changed"
              />
              <AccountPanel
                v-else-if="renderedPanel === 'account'"
                key="account"
                :me="me"
                @close="panel = ''"
                @updated="
                  (user) => {
                    me = user;
                    changed();
                  }
                "
                @logout="logout"
              />
            </Transition>
          </div>
        </Transition>
      </main>
    </Transition>
  </div>
</template>
