<script setup lang="ts">
import { ref, onMounted, watch } from "vue";
import {
  ArrowLeft,
  Users,
  Layers,
  Activity,
  List,
  Search,
  RefreshCw,
} from "lucide-vue-next";
import { api, errorText, dateTime } from "./api";
import type { User, Room, Stats } from "./types";
import ConfirmButton from "./ConfirmButton.vue";
defineProps<{ me: User }>();
const emit = defineEmits<{ close: []; changed: []; sessionChanged: [] }>();
const tab = ref("overview"),
  stats = ref<Stats>(),
  users = ref<User[]>([]),
  rooms = ref<Room[]>([]),
  entries = ref<
    {
      id: number;
      actor: string;
      action: string;
      target: string;
      created_at: number;
    }[]
  >([]),
  offset = ref(0),
  query = ref(""),
  error = ref(""),
  busy = ref(false);
let serial = 0;
const paneDirection = ref("forward");
async function load() {
  const version = ++serial;
  busy.value = true;
  error.value = "";
  try {
    const [s, data] = await Promise.all([
      api<Stats>("/admin/stats"),
      tab.value === "overview"
        ? Promise.resolve(null)
        : api<unknown>(
            "/admin/" +
              tab.value +
              "?offset=" +
              offset.value +
              "&q=" +
              encodeURIComponent(query.value),
          ),
    ]);
    if (version !== serial) return;
    stats.value = s;
    if (tab.value === "users") users.value = data as User[];
    if (tab.value === "rooms") rooms.value = data as Room[];
    if (tab.value === "audit") entries.value = data as typeof entries.value;
  } catch (e) {
    if (version === serial) error.value = errorText(e);
  } finally {
    if (version === serial) busy.value = false;
  }
}
watch(tab, (value, previous) => {
  const order = ["overview", "users", "rooms", "audit"];
  paneDirection.value =
    order.indexOf(value) < order.indexOf(previous) ? "back" : "forward";
  offset.value = 0;
  query.value = "";
  load();
});
async function mutate(path: string, data: unknown, session = false) {
  busy.value = true;
  error.value = "";
  try {
    await api("/admin/" + path, "PATCH", data);
    emit("changed");
    if (session) emit("sessionChanged");
    await load();
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
function page(delta: number) {
  offset.value = Math.max(0, offset.value + delta);
  load();
}
const count = () =>
  tab.value === "users"
    ? users.value.length
    : tab.value === "rooms"
      ? rooms.value.length
      : entries.value.length;
const titles: Record<string, string> = {
  overview: "概览",
  users: "用户管理",
  rooms: "群聊管理",
  audit: "操作日志",
};
onMounted(load);
</script>
<template>
  <main class="admin-layout">
    <aside class="admin-nav panel">
      <button @click="emit('close')"><ArrowLeft :size="17" /> 返回聊天</button>
      <h1>管理后台</h1>
      <button
        :class="{ selected: tab === 'overview' }"
        @click="tab = 'overview'"
      >
        <Activity :size="17" /> 概览</button
      ><button :class="{ selected: tab === 'users' }" @click="tab = 'users'">
        <Users :size="17" /> 用户</button
      ><button :class="{ selected: tab === 'rooms' }" @click="tab = 'rooms'">
        <Layers :size="17" /> 群聊</button
      ><button :class="{ selected: tab === 'audit' }" @click="tab = 'audit'">
        <List :size="17" /> 操作日志
      </button>
    </aside>
    <section
      :key="tab"
      class="admin-main panel"
      :class="'pane-' + paneDirection"
    >
      <div class="section-head">
        <h2>{{ titles[tab] }}</h2>
        <button
          class="icon-btn"
          :disabled="busy"
          aria-label="刷新"
          @click="load"
        >
          <RefreshCw :size="17" />
        </button>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <template v-if="tab === 'overview' && stats"
        ><div class="stats-grid">
          <article>
            <span>用户</span><strong>{{ stats.users }}</strong>
            <p>{{ stats.online }} 人在线</p>
          </article>
          <article>
            <span>群聊</span><strong>{{ stats.groups }}</strong>
          </article>
          <article>
            <span>消息</span
            ><strong>{{ stats.messages.toLocaleString() }}</strong>
          </article>
          <article>
            <span>实时连接</span><strong>{{ stats.connections }}</strong>
          </article>
        </div>
        <h3 class="subheading">注册设置</h3>
        <div class="setting-row">
          <div>
            <strong>开放注册</strong>
            <p>
              {{ stats.registration ? "允许新用户注册" : "已关闭新用户注册" }}
            </p>
          </div>
          <button
            class="switch"
            role="switch"
            :aria-checked="stats.registration"
            aria-label="开放注册"
            :disabled="busy"
            @click="mutate('settings', { registration: !stats!.registration })"
          >
            <span />
          </button>
        </div>
        <h3 class="subheading">运行状态</h3>
        <dl class="runtime-stats">
          <div>
            <dt>Go 堆内存</dt>
            <dd>{{ (stats.heap_bytes / 1048576).toFixed(1) }} MiB</dd>
          </div>
          <div>
            <dt>Goroutine</dt>
            <dd>{{ stats.goroutines }}</dd>
          </div>
          <div>
            <dt>运行时间</dt>
            <dd>
              {{ Math.floor(stats.uptime_seconds / 3600) }}h
              {{ Math.floor((stats.uptime_seconds % 3600) / 60) }}m
            </dd>
          </div>
          <div>
            <dt>慢连接断开次数</dt>
            <dd>{{ stats.dropped_connections }}</dd>
          </div>
        </dl></template
      ><template v-else-if="tab === 'users'"
        ><form
          class="admin-search inline-search"
          @submit.prevent="
            offset = 0;
            load();
          "
        >
          <input
            v-model="query"
            placeholder="搜索用户名或昵称"
            aria-label="搜索用户"
          /><button class="icon-btn" aria-label="搜索" :disabled="busy">
            <Search :size="18" />
          </button>
        </form>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>角色</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id">
                <td>
                  <strong>{{ u.name }}</strong>
                  <p>@{{ u.username }}</p>
                </td>
                <td>{{ u.role === "admin" ? "管理员" : "用户" }}</td>
                <td>
                  {{ u.disabled ? "已停用" : u.online ? "在线" : "离线" }}
                </td>
                <td class="table-actions">
                  <ConfirmButton
                    :label="u.disabled ? '启用' : '停用'"
                    :disabled="busy"
                    @confirm="
                      mutate(
                        'users/' + u.id,
                        { disabled: !u.disabled },
                        u.id === me.id,
                      )
                    "
                  /><ConfirmButton
                    :label="u.role === 'admin' ? '设为用户' : '设为管理员'"
                    :disabled="busy"
                    @confirm="
                      mutate(
                        'users/' + u.id,
                        { role: u.role === 'admin' ? 'user' : 'admin' },
                        u.id === me.id,
                      )
                    "
                  />
                </td>
              </tr>
            </tbody>
          </table></div></template
      ><template v-else-if="tab === 'rooms'"
        ><div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>群名称</th>
                <th>成员</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in rooms" :key="r.id">
                <td>{{ r.name }}</td>
                <td>{{ r.member_count }}</td>
                <td>{{ r.archived ? "已归档" : "正常" }}</td>
                <td>
                  <ConfirmButton
                    :label="r.archived ? '恢复' : '归档'"
                    :disabled="busy"
                    @confirm="
                      mutate('rooms/' + r.id, { archived: !r.archived })
                    "
                  />
                </td>
              </tr>
            </tbody>
          </table></div></template
      ><template v-else-if="tab === 'audit'"
        ><div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>时间</th>
                <th>操作者</th>
                <th>操作</th>
                <th>对象</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="entry in entries" :key="entry.id">
                <td>{{ dateTime(entry.created_at) }}</td>
                <td>{{ entry.actor }}</td>
                <td>{{ entry.action }}</td>
                <td>
                  <code>{{ entry.target }}</code>
                </td>
              </tr>
            </tbody>
          </table>
        </div></template
      >
      <div v-if="tab !== 'overview'" class="pagination">
        <span>{{ count() ? offset + 1 : 0 }}–{{ offset + count() }}</span
        ><button :disabled="offset === 0 || busy" @click="page(-50)">
          上一页</button
        ><button :disabled="count() < 50 || busy" @click="page(50)">
          下一页
        </button>
      </div>
      <p v-if="tab !== 'overview' && !count() && !busy" class="muted-note">
        暂无记录
      </p>
    </section>
  </main>
</template>
