<script setup lang="ts">
import { ref, watch } from "vue";
import { X, UserPlus } from "lucide-vue-next";
import { api, errorText } from "./api";
import type { Room, User } from "./types";
import ConfirmButton from "./ConfirmButton.vue";
const props = defineProps<{ room: Room; me: User; friends: User[] }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const members = ref<User[]>([]),
  error = ref(""),
  busy = ref(false),
  name = ref(""),
  invite = ref(""),
  owner = ref("");
let serial = 0;
async function load() {
  const current = ++serial;
  try {
    const result = await api<User[]>("/rooms/" + props.room.id + "/members");
    if (current === serial) members.value = result;
  } catch (e) {
    if (current === serial) error.value = errorText(e);
  }
}
watch(
  () => props.room,
  () => {
    name.value = props.room.name;
    load();
  },
  { immediate: true },
);
async function mutate(
  path: string,
  method: string,
  data?: unknown,
  close = false,
) {
  busy.value = true;
  error.value = "";
  try {
    await api("/rooms/" + props.room.id + path, method, data);
    emit("changed");
    if (close) emit("close");
    else await load();
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <aside class="side-panel panel">
    <div class="section-head">
      <h2>{{ room.kind === "group" ? "群聊详情" : "联系人详情" }}</h2>
      <button class="icon-btn" aria-label="关闭" @click="emit('close')">
        <X :size="18" />
      </button>
    </div>
    <div class="side-body">
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="detail-title">
        <span class="avatar">{{ room.name.slice(0, 1) }}</span>
        <h2>{{ room.name }}</h2>
        <p>
          {{ room.kind === "group" ? members.length + " 位成员" : "私聊"
          }}{{ room.archived ? " · 已归档" : "" }}
        </p>
      </div>
      <template v-if="room.kind === 'group' && room.owner === me.id"
        ><form class="stack" @submit.prevent="mutate('', 'PATCH', { name })">
          <label>群名称<input v-model="name" maxlength="48" required /></label
          ><button :disabled="busy">保存名称</button>
        </form>
        <form
          v-if="!room.archived"
          class="stack"
          @submit.prevent="mutate('/members', 'POST', { user_id: invite })"
        >
          <label
            >邀请好友<select v-model="invite" required>
              <option value="" disabled>选择好友</option>
              <option
                v-for="u in friends.filter(
                  (u) => !u.disabled && !members.some((m) => m.id === u.id),
                )"
                :key="u.id"
                :value="u.id"
              >
                {{ u.name }}
              </option>
            </select></label
          ><button :disabled="busy"><UserPlus :size="15" /> 添加成员</button>
        </form></template
      >
      <h3 class="subheading">{{ room.kind === "group" ? "成员" : "账号" }}</h3>
      <div v-for="u in members" :key="u.id" class="person-row">
        <span class="avatar small">{{ u.name.slice(0, 1) }}</span>
        <div class="person-info">
          <strong>{{ u.name }}</strong>
          <p>@{{ u.username }}{{ u.disabled ? " · 已停用" : "" }}</p>
        </div>
        <small v-if="room.kind === 'group' && u.id === room.owner">群主</small
        ><ConfirmButton
          v-else-if="room.kind === 'group' && me.id === room.owner"
          label="移除"
          :disabled="busy"
          @confirm="mutate('/members/' + u.id, 'DELETE')"
        />
      </div>
      <template v-if="room.kind === 'group'"
        ><template v-if="room.owner === me.id"
          ><div class="stack">
            <label
              >转让群主<select v-model="owner">
                <option value="" disabled>选择成员</option>
                <option
                  v-for="u in members.filter(
                    (u) => u.id !== me.id && !u.disabled,
                  )"
                  :key="u.id"
                  :value="u.id"
                >
                  {{ u.name }}
                </option>
              </select></label
            ><ConfirmButton
              label="转让"
              :disabled="busy || !owner"
              @confirm="mutate('', 'PATCH', { owner })"
            /><ConfirmButton
              :label="room.archived ? '恢复群聊' : '归档群聊'"
              :disabled="busy"
              @confirm="mutate('', 'PATCH', { archived: !room.archived })"
            /></div></template
        ><ConfirmButton
          v-else
          label="退出群聊"
          :disabled="busy"
          @confirm="mutate('/members/' + me.id, 'DELETE', undefined, true)"
      /></template>
    </div>
  </aside>
</template>
