<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import ClientsPanel from './components/ClientsPanel.vue'
import CouponsPanel from './components/CouponsPanel.vue'
import StatisticsPanel from './components/StatisticsPanel.vue'

type Session = {
  user: { id: string; login: string; name: string }
  expires_at: string
  csrf_token: string
}
const section = ref<'clients' | 'coupons' | 'statistics'>('coupons')
const couponsPanel = ref<InstanceType<typeof CouponsPanel> | null>(null)
async function showCoupons(usage: 'all' | 'used' | 'unused' = 'all') {
  section.value = 'coupons'
  await nextTick()
  couponsPanel.value?.showList(usage)
}
function home(event: MouseEvent) {
  if (session.value) {
    event.preventDefault()
    void showCoupons()
  }
}
const state = ref<'loading' | 'unavailable' | 'guest' | 'member'>('loading')
const session = ref<Session | null>(null)
const loginName = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
let expiryTimer: ReturnType<typeof setTimeout> | undefined
let refreshController: AbortController | undefined

function showGuest(message = '') {
  clearTimeout(expiryTimer)
  session.value = null
  state.value = 'guest'
  error.value = message
}
function acceptSession(value: Session) {
  clearTimeout(expiryTimer)
  if (session.value?.user.id !== value.user.id) section.value = 'coupons'
  session.value = value
  state.value = 'member'
  error.value = ''
  expiryTimer = setTimeout(
    () => showGuest('Сеанс завершен. Войдите снова.'),
    Math.max(0, Date.parse(value.expires_at) - Date.now()),
  )
}
async function refresh() {
  if (busy.value) return
  refreshController?.abort()
  const controller = new AbortController()
  refreshController = controller
  try {
    const response = await fetch('/api/auth/session', {
      credentials: 'same-origin',
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(10000)]),
    })
    if (controller.signal.aborted) return
    if (response.status === 401) {
      showGuest(session.value ? 'Сеанс завершен. Войдите снова.' : '')
    } else if (response.ok) {
      const value = (await response.json()) as Session
      if (!controller.signal.aborted) acceptSession(value)
    } else {
      throw new Error('unavailable')
    }
  } catch {
    if (controller.signal.aborted) return
    if (!session.value) state.value = 'unavailable'
    error.value = 'Не удалось связаться с сервисом. Попробуйте еще раз.'
  }
}
async function login() {
  if (busy.value) return
  refreshController?.abort()
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        login: loginName.value,
        password: password.value,
      }),
      signal: AbortSignal.timeout(10000),
    })
    if (response.ok) {
      acceptSession((await response.json()) as Session)
      password.value = ''
    } else {
      error.value =
        response.status === 401
          ? 'Неверный логин или пароль.'
          : response.status === 429
            ? 'Слишком много попыток входа. Повторите через 15 минут.'
            : response.status === 400
              ? 'Проверьте логин и пароль.'
              : 'Не удалось войти. Попробуйте еще раз.'
    }
  } catch {
    error.value = 'Не удалось связаться с сервисом. Попробуйте еще раз.'
  } finally {
    busy.value = false
  }
}
async function logout() {
  if (busy.value || !session.value) return
  refreshController?.abort()
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/api/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': session.value.csrf_token },
      signal: AbortSignal.timeout(10000),
    })
    if (response.ok || response.status === 401) showGuest()
    else error.value = 'Не удалось завершить сеанс. Попробуйте еще раз.'
  } catch {
    error.value =
      'Не удалось завершить сеанс. Проверьте соединение и повторите.'
  } finally {
    busy.value = false
  }
}
onMounted(() => {
  void refresh()
  window.addEventListener('focus', refresh)
})
onBeforeUnmount(() => {
  refreshController?.abort()
  clearTimeout(expiryTimer)
  window.removeEventListener('focus', refresh)
})
</script>

<template>
  <div class="page" :class="{ 'member-page': state === 'member' }">
    <header class="header app-header">
      <a class="brand" href="/" aria-label="Referflow — главная" @click="home"
        ><img src="/favicon.svg" alt="" width="32" height="32" /><span
          >referflow</span
        ></a
      >
      <nav v-if="session" class="cabinet-nav" aria-label="Разделы кабинета">
        <button
          type="button"
          :aria-pressed="section === 'coupons'"
          @click="section = 'coupons'"
        >
          Купоны
        </button>
        <button
          type="button"
          :aria-pressed="section === 'clients'"
          @click="section = 'clients'"
        >
          Клиенты
        </button>
        <button
          type="button"
          :aria-pressed="section === 'statistics'"
          @click="section = 'statistics'"
        >
          Статистика
        </button>
      </nav>
      <details v-if="session" class="account-menu">
        <summary>
          <span class="account-avatar" aria-hidden="true">{{
            session.user.name.slice(0, 1).toUpperCase()
          }}</span><span>Профиль</span>
        </summary>
        <div class="account-content">
          <strong>{{ session.user.name }}</strong>
          <p class="muted">{{ session.user.login }}</p>
          <button
            class="secondary"
            type="button"
            :disabled="busy"
            @click="logout"
          >
            {{ busy ? 'Выходим…' : 'Выйти' }}
          </button>
        </div>
      </details>
      <span v-else class="header-note">Кабинет специалиста</span>
    </header>
    <main v-if="state === 'loading'" class="center-state" aria-busy="true">
      <p role="status">Открываем ваш кабинет…</p>
    </main>
    <main v-else-if="state === 'unavailable'" class="center-state">
      <h1>Не удалось открыть кабинет</h1>
      <p class="muted" role="alert">{{ error }}</p>
      <button class="primary" type="button" @click="refresh">
        Попробовать снова
      </button>
    </main>
    <main v-else-if="state === 'guest'" class="simple-login">
      <section class="login-card" aria-labelledby="login-title">
        <h1 id="login-title">Вход в кабинет</h1>
        <p class="muted">Войдите, чтобы выдавать и использовать купоны.</p>
        <form :aria-busy="busy" @submit.prevent="login">
          <label for="login">Логин</label
          ><input
            id="login"
            v-model="loginName"
            name="login"
            type="text"
            autocomplete="username"
            autocapitalize="none"
            :spellcheck="false"
            required
            maxlength="508"
            :disabled="busy"
            :aria-describedby="error ? 'login-error' : undefined"
          />
          <label for="password">Пароль</label
          ><input
            id="password"
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            :disabled="busy"
            :aria-describedby="error ? 'login-error' : undefined"
          />
          <p v-if="error" id="login-error" class="error" role="alert">
            {{ error }}
          </p>
          <button class="primary" type="submit" :disabled="busy">
            {{ busy ? 'Входим…' : 'Войти' }}
          </button>
        </form>
        <p class="login-help">Нет доступа? Обратитесь к администратору.</p>
      </section>
    </main>
    <main v-else-if="session" class="cabinet workspace">
      <h1 class="sr-only">Кабинет специалиста</h1>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <CouponsPanel
        v-show="section === 'coupons'"
        ref="couponsPanel"
        :key="session.user.id"
        :csrf-token="session.csrf_token"
        @expired="showGuest('Сеанс завершен. Войдите снова.')"
      />
      <ClientsPanel
        v-if="section === 'clients'"
        :key="session.user.id"
        :csrf-token="session.csrf_token"
        @expired="showGuest('Сеанс завершен. Войдите снова.')"
      />
      <StatisticsPanel
        v-if="section === 'statistics'"
        :key="session.user.id"
        @coupons="showCoupons"
        @expired="showGuest('Сеанс завершен. Войдите снова.')"
      />
    </main>
    <footer class="app-footer">
      <span>Referflow</span><span>Купоны и рекомендации</span>
    </footer>
  </div>
</template>
