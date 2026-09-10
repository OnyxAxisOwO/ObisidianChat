<script setup lang="ts">
import { ref, onBeforeUnmount } from "vue";
defineProps<{ label: string; disabled?: boolean }>();
const emit = defineEmits<{ confirm: [] }>();
const armed = ref(false);
let timer: ReturnType<typeof setTimeout>;
function click() {
  if (armed.value) {
    armed.value = false;
    clearTimeout(timer);
    emit("confirm");
  } else {
    armed.value = true;
    timer = setTimeout(() => (armed.value = false), 4000);
  }
}
onBeforeUnmount(() => clearTimeout(timer));
</script>
<template>
  <button class="danger" :class="{ armed }" :disabled="disabled" @click="click">
    {{ armed ? "确认" + label : label }}
  </button>
</template>
