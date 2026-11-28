<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import CouponTemplatesPanel from './components/CouponTemplatesPanel.vue'
import type { IssuanceTerms } from './coupon-terms'
import CouponRedemptionPanel from './components/CouponRedemptionPanel.vue'
import ClientsPanel from './components/ClientsPanel.vue'
import CouponsPanel from './components/CouponsPanel.vue'
import StatisticsPanel from './components/StatisticsPanel.vue'

type Session = {
  user: { id: string; login: string; name: string }
  expires_at: string
  csrf_token: string
}
const section = ref<
  'clients' | 'coupons' | 'statistics' | 'templates' | 'redeem'
>('coupons')
const clientsVisited = ref(false)
const keyboardOpen = ref(false)
const scrollPositions: Partial<Record<typeof section.value, number>> = {}
async function selectSection(next: typeof section.value) {
  if (next === section.value) return
  scrollPositions[section.value] = window.scrollY
  if (next === 'clients') clientsVisited.value = true
  section.value = next
  await nextTick()
  window.scrollTo({ top: scrollPositions[next] ?? 0 })
}
function updateKeyboard() {
  const viewport = window.visualViewport
  const focused = document.activeElement?.matches(
    'input:not([type=checkbox]):not([type=radio]), textarea, select',
  )
  keyboardOpen.value = Boolean(
    focused &&
    viewport &&
    window.innerHeight - viewport.height - viewport.offsetTop > 150,
  )
}
let keyboardFrame = 0
function queueKeyboardCheck() {
  cancelAnimationFrame(keyboardFrame)
  keyboardFrame = requestAnimationFrame(updateKeyboard)
}
const couponsPanel = ref<InstanceType<typeof CouponsPanel> | null>(null)
async function showCoupons(usage: 'all' | 'used' | 'unused' = 'all') {
  await selectSection('coupons')
  await nextTick()
  couponsPanel.value?.showList(usage)
}
async function issueTemplate(
  terms: IssuanceTerms,
  complete: (opened: boolean) => void,
) {
  const opened = await couponsPanel.value?.create(terms)
  if (opened) {
    await selectSection('coupons')
    await nextTick()
    await couponsPanel.value?.focusEditor()
  }
  complete(Boolean(opened))
}
async function templateCoupons(
  id: string,
  complete: (opened: boolean) => void,
) {
  const opened = await couponsPanel.value?.showList('all', id)
  if (opened) await selectSection('coupons')
  complete(Boolean(opened))
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
  clientsVisited.value = false
  keyboardOpen.value = false
  for (const key of Object.keys(scrollPositions))
    delete scrollPositions[key as typeof section.value]
  session.value = null
  state.value = 'guest'
  error.value = message
}
function acceptSession(value: Session) {
  clearTimeout(expiryTimer)
  if (session.value?.user.id !== value.user.id) {
    section.value = 'coupons'
    clientsVisited.value = false
  }
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
  window.visualViewport?.addEventListener('resize', queueKeyboardCheck)
  document.addEventListener('focusin', queueKeyboardCheck)
  document.addEventListener('focusout', queueKeyboardCheck)
})
onBeforeUnmount(() => {
  refreshController?.abort()
  clearTimeout(expiryTimer)
  cancelAnimationFrame(keyboardFrame)
  window.visualViewport?.removeEventListener('resize', queueKeyboardCheck)
  document.removeEventListener('focusin', queueKeyboardCheck)
  document.removeEventListener('focusout', queueKeyboardCheck)
  window.removeEventListener('focus', refresh)
})
</script>

<template>
  <div
    class="page"
    :class="{
      'member-page': state === 'member',
      'keyboard-open': keyboardOpen,
    }"
  >
    <header class="header app-header">
      <a class="brand" href="/" aria-label="Referflow — главная" @click="home"
        ><img src="/favicon.svg" alt="" width="32" height="32" /><span
          >referflow</span
        ></a
      >
      <nav v-if="session" class="cabinet-nav" aria-label="Разделы кабинета">
        <button
          type="button"
          :aria-pressed="section === 'redeem'"
          @click="selectSection('redeem')"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5M8 12l3 3 5-6" /></svg
          ><span>Применить</span>
        </button>
        <button
          type="button"
          :aria-pressed="section === 'coupons'"
          @click="selectSection('coupons')"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 5h16v5a2 2 0 0 0 0 4v5H4v-5a2 2 0 0 0 0-4V5Z" />
            <path d="M9 5v14" stroke-dasharray="2 3" /></svg
          ><span>Купоны</span>
        </button>
        <button
          type="button"
          :aria-pressed="section === 'templates'"
          @click="selectSection('templates')"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <rect x="7" y="7" width="13" height="14" rx="2" />
            <path d="M16 7V3H3v14h4M11 12h5m-5 4h5" /></svg
          ><span>Шаблоны</span>
        </button>
        <button
          type="button"
          :aria-pressed="section === 'clients'"
          @click="selectSection('clients')"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="9" cy="8" r="3" />
            <path
              d="M3 20v-2a6 6 0 0 1 12 0v2M16 5a3 3 0 0 1 0 6m2 4a5 5 0 0 1 3 5"
            /></svg
          ><span>Клиенты</span>
        </button>
        <button
          type="button"
          :aria-pressed="section === 'statistics'"
          @click="selectSection('statistics')"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 3v18h17M9 17v-5m5 5V8m5 9V4" /></svg
          ><span>Статистика</span>
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
        @templates="selectSection('templates')"
        @redeem="selectSection('redeem')"
      />
      <CouponRedemptionPanel
        v-show="section === 'redeem'"
        :key="session.user.id"
        :csrf-token="session.csrf_token"
        @expired="showGuest('Сеанс завершен. Войдите снова.')"
      />
      <CouponTemplatesPanel
        v-show="section === 'templates'"
        :key="session.user.id"
        :csrf-token="session.csrf_token"
        :active="section === 'templates'"
        @issue="issueTemplate"
        @coupons="templateCoupons"
        @expired="showGuest('Сеанс завершен. Войдите снова.')"
      />
      <ClientsPanel
        v-if="clientsVisited"
        v-show="section === 'clients'"
        :key="session.user.id"
        :active="section === 'clients'"
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
