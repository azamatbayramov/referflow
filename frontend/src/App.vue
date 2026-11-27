<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

type Session = {
  user: { id: string; login: string; name: string }
  expires_at: string
  csrf_token: string
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
  <div class="page">
    <header class="header">
      <a class="brand" href="/" aria-label="Referflow — главная">
        <img src="/favicon.svg" alt="" width="36" height="36" />
        <span>referflow</span>
      </a>
      <span class="header-note">Кабинет специалиста</span>
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
    <main v-else-if="state === 'guest'" class="login-layout">
      <section class="welcome" aria-labelledby="welcome-title">
        <p class="eyebrow"><span class="dot"></span> Хорошие связи</p>
        <h1 id="welcome-title">
          Рекомендации<br />начинаются<br /><span>с доверия.</span>
        </h1>
        <p class="intro">
          Ваше пространство для работы с рекомендациями клиентов.
        </p>
        <div class="welcome-mark" aria-hidden="true">
          <span>r</span>
          <span>
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
              <path
                d="M6 18 18 6M6 6h12v12"
                stroke="currentColor"
                stroke-width="1.5"
              />
            </svg>
          </span>
        </div>
      </section>
      <section class="login-card" aria-labelledby="login-title">
        <p class="card-label">РАДЫ ВАС ВИДЕТЬ</p>
        <h2 id="login-title">Вход в кабинет</h2>
        <p class="muted">Используйте свою учетную запись специалиста.</p>
        <form :aria-busy="busy" @submit.prevent="login">
          <label for="login">Логин</label>
          <input
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
          <label for="password">Пароль</label>
          <input
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
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              aria-hidden="true"
            >
              <path
                d="M6 18 18 6M6 6h12v12"
                stroke="currentColor"
                stroke-width="1.5"
              />
            </svg>
          </button>
        </form>
        <p class="login-help">Если нет доступа, обратитесь к администратору.</p>
      </section>
    </main>
    <main v-else-if="session" class="cabinet">
      <div class="cabinet-heading">
        <div>
          <p class="eyebrow"><span class="dot"></span> Личное пространство</p>
          <h1>Здравствуйте,<br />{{ session.user.name }}.</h1>
        </div>
        <button
          class="secondary"
          type="button"
          :disabled="busy"
          @click="logout"
        >
          {{ busy ? 'Выходим…' : 'Выйти' }}
        </button>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <section class="profile" aria-labelledby="profile-title">
        <div class="avatar" aria-hidden="true">
          {{ session.user.name.slice(0, 1).toUpperCase() }}
        </div>
        <div class="profile-details">
          <h2 id="profile-title">Ваш профиль</h2>
          <p>{{ session.user.name }}</p>
          <p class="muted">{{ session.user.login }}</p>
        </div>
        <span class="session-badge"
          ><span class="dot"></span> Сеанс активен</span
        >
      </section>
      <p class="session-help">
        Завершайте сеанс после работы на общем устройстве.
      </p>
    </main>
    <footer>
      <span>Referflow</span><span>Сделано для хороших связей</span>
    </footer>
  </div>
</template>
