<script setup lang="ts">
import { computed, ref } from "vue";
import { Search, X, Users, Plus } from "lucide-vue-next";
import { api, errorText } from "./api";
import type { User, FriendData } from "./types";
import ConfirmButton from "./ConfirmButton.vue";
const props = defineProps<{
  me: User;
  data: FriendData;
  mode: "friends" | "group";
}>();
const emit = defineEmits<{ close: []; changed: []; room: [id: string] }>();
const query = ref(""),
  found = ref<User[]>([]),
  searched = ref(false),
  error = ref(""),
  note = ref(""),
  busy = ref(false),
  name = ref(""),
  members = ref<string[]>([]);
const incoming = computed(() =>
  props.data.requests.filter((r) => r.recipient === props.me.id),
);
const outgoing = computed(() =>
  props.data.requests.filter((r) => r.sender === props.me.id),
);
async function run(action: () => Promise<void>) {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    await action();
    emit("changed");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
async function search() {
  await run(async () => {
    found.value = await api<User[]>(
      "/users?q=" + encodeURIComponent(query.value),
    );
    searched.value = true;
  });
}
function add(user: User) {
  return run(async () => {
    await api("/friends/requests", "POST", { user_id: user.id });
    note.value = "申请已发送";
  });
}
function resolve(id: string, action: string) {
  return run(async () => {
    const res = await api<{ room_id: string }>(
      "/friends/requests/" + id,
      "POST",
      { action },
    );
    if (res.room_id) emit("room", res.room_id);
  });
}
function remove(id: string) {
  return run(async () => {
    await api("/friends/" + id, "DELETE");
  });
}
function create() {
  return run(async () => {
    const room = await api<{ id: string }>("/rooms", "POST", {
      name: name.value,
      members: members.value,
    });
    emit("room", room.id);
    emit("close");
  });
}
</script>
<template>
  <aside class="side-panel panel">
    <div class="section-head">
      <h2>{{ mode === "group" ? "创建群聊" : "好友管理" }}</h2>
      <button class="icon-btn" aria-label="关闭" @click="emit('close')">
        <X :size="18" />
      </button>
    </div>
    <div class="side-body">
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="note" class="notice" role="status">{{ note }}</p>
      <template v-if="mode === 'friends'"
        ><form class="inline-search" @submit.prevent="search">
          <input
            v-model="query"
            placeholder="用户名或昵称（至少 2 位）"
            aria-label="搜索用户"
            minlength="2"
            maxlength="32"
            required
          /><button class="icon-btn" aria-label="搜索" :disabled="busy">
            <Search :size="18" />
          </button>
        </form>
        <div v-for="user in found" :key="user.id" class="person-row">
          <span class="avatar small">{{ user.name.slice(0, 1) }}</span>
          <div class="person-info">
            <strong>{{ user.name }}</strong>
            <p>@{{ user.username }}</p>
          </div>
          <button
            v-if="
              !data.friends.some((f) => f.id === user.id) &&
              !data.requests.some(
                (r) => r.sender === user.id || r.recipient === user.id,
              )
            "
            class="icon-btn"
            aria-label="添加好友"
            :disabled="busy"
            @click="add(user)"
          >
            <Plus :size="17" /></button
          ><small v-else>{{
            data.friends.some((f) => f.id === user.id) ? "已添加" : "待处理"
          }}</small>
        </div>
        <p v-if="searched && !found.length" class="muted-note">未找到用户</p>
        <h3 class="subheading">
          收到的申请 <span>{{ incoming.length }}</span>
        </h3>
        <p v-if="!incoming.length" class="muted-note">暂无申请</p>
        <div v-for="r in incoming" :key="r.id" class="request-row">
          <div>
            <strong>{{ r.name }}</strong>
            <p>@{{ r.username }}</p>
          </div>
          <div class="button-row">
            <button
              class="primary"
              :disabled="busy"
              @click="resolve(r.id, 'accept')"
            >
              接受</button
            ><button :disabled="busy" @click="resolve(r.id, 'reject')">
              拒绝
            </button>
          </div>
        </div>
        <h3 class="subheading">
          发出的申请 <span>{{ outgoing.length }}</span>
        </h3>
        <p v-if="!outgoing.length" class="muted-note">暂无申请</p>
        <div v-for="r in outgoing" :key="r.id" class="person-row">
          <div class="person-info">
            <strong>{{ r.name }}</strong>
            <p>@{{ r.username }}</p>
          </div>
          <button :disabled="busy" @click="resolve(r.id, 'cancel')">
            撤回
          </button>
        </div>
        <h3 class="subheading">
          好友 <span>{{ data.friends.length }}</span>
        </h3>
        <div v-for="u in data.friends" :key="u.id" class="person-row">
          <span class="avatar small">{{ u.name.slice(0, 1) }}</span>
          <div class="person-info">
            <strong>{{ u.name }}</strong>
            <p>@{{ u.username }}</p>
          </div>
          <ConfirmButton
            label="移除"
            :disabled="busy"
            @confirm="remove(u.id)"
          /></div
      ></template>
      <form v-else class="stack" @submit.prevent="create">
        <label>群名称<input v-model="name" maxlength="48" required /></label>
        <h3 class="subheading">邀请好友</h3>
        <p v-if="!data.friends.length" class="muted-note">
          暂无好友，可先创建群聊。
        </p>
        <label
          v-for="u in data.friends.filter((f) => !f.disabled)"
          :key="u.id"
          class="check-row"
          ><input v-model="members" type="checkbox" :value="u.id" /><span
            class="avatar small"
            >{{ u.name.slice(0, 1) }}</span
          ><span>{{ u.name }}</span></label
        ><button class="primary" :disabled="busy">
          <Users :size="15" /> {{ busy ? "创建中…" : "创建群聊" }}
        </button>
      </form>
    </div>
  </aside>
</template>
