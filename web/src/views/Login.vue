<template>
  <div class="login-page">
    <section class="login-panel">
      <div class="brand-mark">
        <n-icon size="34"><Leaf /></n-icon>
      </div>
      <div class="login-heading">
        <h1>Auto-RSS</h1>
        <p>登录后继续管理订阅、下载和系统配置。</p>
      </div>

      <n-alert v-if="authEnabled" type="warning" :show-icon="true" class="security-alert">
        启用认证前请确认已修改 JWT_SECRET 和 JWT_PASSWORD。
      </n-alert>

      <n-form ref="formRef" :model="form" :rules="rules" size="large" @submit.prevent="handleLogin">
        <n-form-item path="username" label="用户名">
          <n-input v-model:value="form.username" placeholder="admin" autocomplete="username">
            <template #prefix>
              <n-icon><PersonOutline /></n-icon>
            </template>
          </n-input>
        </n-form-item>

        <n-form-item path="password" label="密码">
          <n-input
            v-model:value="form.password"
            type="password"
            placeholder="请输入密码"
            show-password-on="click"
            autocomplete="current-password"
            @keyup.enter="handleLogin"
          >
            <template #prefix>
              <n-icon><LockClosedOutline /></n-icon>
            </template>
          </n-input>
        </n-form-item>

        <n-button type="primary" block size="large" :loading="loading" @click="handleLogin">
          <template #icon>
            <n-icon><LogInOutline /></n-icon>
          </template>
          登录
        </n-button>
      </n-form>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, useMessage, type FormInst, type FormRules } from 'naive-ui'
import { Leaf, LockClosedOutline, LogInOutline, PersonOutline } from '@vicons/ionicons5'
import { authApi } from '@/api'

const router = useRouter()
const route = useRoute()
const message = useMessage()

const formRef = ref<FormInst | null>(null)
const loading = ref(false)
const authEnabled = ref(true)
const form = reactive({
  username: 'admin',
  password: ''
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: ['blur', 'input'] }],
  password: [{ required: true, message: '请输入密码', trigger: ['blur', 'input'] }]
}

onMounted(async () => {
  try {
    const status = await authApi.status(true)
    authEnabled.value = status.auth_enabled
    form.username = status.username || form.username
  } catch {
    authEnabled.value = true
  }
})

const handleLogin = async () => {
  await formRef.value?.validate()
  loading.value = true
  try {
    await authApi.login(form.username, form.password)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/rss-sources'
    await router.replace(redirect)
  } catch {
    message.error('用户名或密码不正确')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background:
    linear-gradient(135deg, rgba(24, 160, 88, 0.12), rgba(32, 33, 36, 0.08)),
    radial-gradient(circle at 20% 15%, rgba(24, 160, 88, 0.16), transparent 28%),
    #f6f8f7;
}

.login-panel {
  width: min(420px, 100%);
  padding: 32px;
  border: 1px solid rgba(32, 33, 36, 0.08);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.92);
  box-shadow: 0 18px 54px rgba(28, 35, 31, 0.16);
}

.brand-mark {
  width: 56px;
  height: 56px;
  display: grid;
  place-items: center;
  margin-bottom: 18px;
  color: #18a058;
  border-radius: 8px;
  background: rgba(24, 160, 88, 0.12);
}

.login-heading {
  margin-bottom: 22px;
}

.login-heading h1 {
  margin: 0;
  color: #1f2a24;
  font-size: 30px;
  font-weight: 700;
  line-height: 1.1;
}

.login-heading p {
  margin: 8px 0 0;
  color: #5e6963;
  font-size: 14px;
  line-height: 1.6;
}

.security-alert {
  margin-bottom: 20px;
}

@media (max-width: 480px) {
  .login-page {
    padding: 16px;
    align-items: stretch;
  }

  .login-panel {
    align-self: center;
    padding: 24px;
  }
}
</style>
