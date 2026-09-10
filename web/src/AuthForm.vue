<script setup lang="ts">
import { ref, onMounted } from "vue";
import { api, errorText } from "./api";
import type { User } from "./types";
const emit = defineEmits<{ login: [user: User] }>();
const mode = ref("login"),
  setup = ref(false),
  registration = ref(false),
  loaded = ref(false),
  busy = ref(false),
  error = ref("");
const username = ref(""),
  name = ref(""),
  password = ref(""),
  token = ref("");
async function status() {
  try {
    const s = await api<{ setup_required: boolean; registration: boolean }>(
      "/status",
    );
    setup.value = s.setup_required;
    registration.value = s.registration;
    mode.value = s.setup_required ? "setup" : "login";
    loaded.value = true;
    error.value = "";
  } catch (e) {
    error.value = errorText(e);
  }
}
onMounted(status);
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    const payload =
      mode.value === "login"
        ? { username: username.value, password: password.value }
        : {
            username: username.value,
            name: name.value,
            password: password.value,
            token: token.value,
          };
    const user = await api<User>("/" + mode.value, "POST", payload);
    password.value = "";
    token.value = "";
    emit("login", user);
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="auth-wrap">
    <form :key="mode" class="auth-form" @submit.prevent="submit">
      <h2>
        {{
          mode === "setup"
            ? "初始化管理员"
            : mode === "register"
              ? "注册"
              : "登录"
        }}
      </h2>
      <p v-if="setup">填写服务启动时输出的初始化令牌。</p>
      <p v-if="!loaded && !error">连接中…</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button v-if="!loaded && error" type="button" @click="status">重试</button
      ><template v-if="loaded"
        ><label v-if="setup"
          >初始化令牌<input
            v-model="token"
            type="password"
            autocomplete="off"
            required /></label
        ><label
          >用户名<input
            v-model="username"
            autocomplete="username"
            pattern="[A-Za-z0-9_]{3,24}"
            title="3–24 位字母、数字或下划线"
            maxlength="24"
            required /></label
        ><label v-if="mode !== 'login'"
          >昵称<input
            v-model="name"
            maxlength="32"
            autocomplete="nickname"
            required /></label
        ><label
          >密码<input
            v-model="password"
            type="password"
            :autocomplete="
              mode === 'login' ? 'current-password' : 'new-password'
            "
            :minlength="mode === 'login' ? 1 : 10"
            maxlength="128"
            required
          /><small v-if="mode !== 'login'">至少 10 位</small></label
        ><button class="primary" :disabled="busy">
          {{
            busy
              ? "提交中…"
              : mode === "setup"
                ? "创建管理员"
                : mode === "register"
                  ? "注册"
                  : "登录"
          }}</button
        ><button
          v-if="!setup && registration"
          type="button"
          @click="
            mode = mode === 'login' ? 'register' : 'login';
            error = '';
          "
        >
          {{ mode === "login" ? "注册账号" : "返回登录" }}
        </button></template
      >
    </form>
  </div>
</template>
