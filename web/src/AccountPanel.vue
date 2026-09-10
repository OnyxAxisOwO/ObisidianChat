<script setup lang="ts">
import { ref } from "vue";
import { X } from "lucide-vue-next";
import { api, errorText } from "./api";
import type { User } from "./types";
const props = defineProps<{ me: User }>();
const emit = defineEmits<{ close: []; updated: [user: User]; logout: [] }>();
const name = ref(props.me.name),
  password = ref(""),
  oldPassword = ref(""),
  busy = ref(false),
  error = ref(""),
  note = ref("");
async function save() {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const user = await api<User>("/me", "PATCH", {
      name: name.value,
      password: password.value,
      oldPassword: oldPassword.value,
    });
    password.value = "";
    oldPassword.value = "";
    emit("updated", user);
    note.value = "已保存";
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
      <h2>账号设置</h2>
      <button class="icon-btn" aria-label="关闭" @click="emit('close')">
        <X :size="18" />
      </button>
    </div>
    <form class="side-body stack" @submit.prevent="save">
      <p>@{{ me.username }} · {{ me.role === "admin" ? "管理员" : "用户" }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="note" role="status">{{ note }}</p>
      <label
        >昵称<input
          v-model="name"
          maxlength="32"
          required
          autocomplete="nickname" /></label
      ><label
        >原密码<input
          v-model="oldPassword"
          type="password"
          autocomplete="current-password"
          :required="!!password" /></label
      ><label
        >新密码<input
          v-model="password"
          type="password"
          minlength="10"
          maxlength="128"
          autocomplete="new-password"
          placeholder="不修改请留空" /></label
      ><button class="primary" :disabled="busy">
        {{ busy ? "保存中…" : "保存" }}</button
      ><button type="button" @click="emit('logout')">退出登录</button>
    </form>
  </aside>
</template>
