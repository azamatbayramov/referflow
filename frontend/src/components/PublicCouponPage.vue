<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  couponAmount,
  couponLink,
  couponPNG,
  couponQR,
  couponTerms,
  publicStatus,
  validityNote,
  type PublicCoupon,
} from '../public-coupon'

const id =
  window.location.pathname.match(
    /^\/c\/([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\/?$/i,
  )?.[1] ?? ''
const link = id ? couponLink(id) : ''
const qr = computed(() => (link ? couponQR(link) : ''))
const coupon = ref<PublicCoupon | null>(null)
const state = ref<'loading' | 'ready' | 'missing' | 'error'>('loading')
const checked = ref(''),
  notice = ref(''),
  actionError = ref(''),
  exporting = ref(false)
const linkInput = ref<HTMLInputElement | null>(null)
const fallbackCopy = ref(false)
let controller: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined
let alive = true
let lastCheck = 0
const formatTime = (date: Date) =>
  date.toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })

async function load(): Promise<PublicCoupon | null> {
  controller?.abort()
  clearTimeout(timer)
  coupon.value = null
  state.value = 'loading'
  if (!id) {
    state.value = 'missing'
    return null
  }
  const current = new AbortController()
  controller = current
  try {
    const response = await fetch(`/api/public/coupons/${id}`, {
      cache: 'no-store',
      credentials: 'omit',
      referrerPolicy: 'no-referrer',
      signal: AbortSignal.any([current.signal, AbortSignal.timeout(10000)]),
    })
    if (!alive || current.signal.aborted) return null
    if (response.status === 404) {
      state.value = 'missing'
      return null
    }
    if (!response.ok) throw new Error('unavailable')
    const value = (await response.json()) as PublicCoupon
    if (!alive || current.signal.aborted) return null
    coupon.value = value
    state.value = 'ready'
    lastCheck = Date.now()
    checked.value = formatTime(new Date())
    const untilExpiry = value.expires_at
      ? Date.parse(value.expires_at) - Date.now()
      : Infinity
    const delay = Math.min(60000, untilExpiry > 0 ? untilExpiry + 100 : 60000)
    timer = setTimeout(refreshVisible, delay)
    return value
  } catch {
    if (alive && !current.signal.aborted) state.value = 'error'
    return null
  }
}
function refreshVisible() {
  if (document.visibilityState === 'visible') void load()
}
function focus() {
  if (Date.now() - lastCheck > 3000) refreshVisible()
}
function pageShow(event: PageTransitionEvent) {
  if (event.persisted) void load()
}
async function copyLink() {
  actionError.value = ''
  notice.value = ''
  try {
    await navigator.clipboard.writeText(link)
    notice.value = 'Ссылка скопирована. Ее можно отправить клиенту.'
  } catch {
    fallbackCopy.value = true
    await nextTick()
    linkInput.value?.focus()
    linkInput.value?.select()
    notice.value = 'Скопируйте выделенную ссылку.'
  }
}
async function exportCard(kind: 'print' | 'image') {
  if (exporting.value) return
  exporting.value = true
  actionError.value = ''
  notice.value = ''
  try {
    const current = await load()
    if (!current) throw new Error('unavailable')
    await nextTick()
    if (kind === 'print') {
      await Promise.all(
        Array.from(
          document.querySelectorAll<HTMLImageElement>(
            '.public-coupon-card img',
          ),
        ).map((img) => img.decode()),
      )
      window.print()
    } else {
      const blob = await couponPNG(current, qr.value, checked.value)
      if (!alive) return
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `referflow-${current.code}.png`
      a.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      notice.value = 'Изображение сохранено. Статус на копии может измениться.'
    }
  } catch {
    if (alive)
      actionError.value =
        'Не удалось подготовить карточку. Обновите страницу и повторите.'
  } finally {
    if (alive) exporting.value = false
  }
}
onMounted(() => {
  document.title = 'Купон — Referflow'
  void load()
  window.addEventListener('focus', focus)
  window.addEventListener('pageshow', pageShow)
  document.addEventListener('visibilitychange', refreshVisible)
})
onBeforeUnmount(() => {
  alive = false
  controller?.abort()
  clearTimeout(timer)
  window.removeEventListener('focus', focus)
  window.removeEventListener('pageshow', pageShow)
  document.removeEventListener('visibilitychange', refreshVisible)
})
</script>

<template>
  <div class="page public-coupon-page">
    <header class="header public-controls">
      <a class="brand" href="/" aria-label="Referflow — главная"
        ><img src="/favicon.svg" alt="" width="36" height="36" /><span
          >referflow</span
        ></a
      >
      <a class="public-login" href="/">Вход специалиста</a>
    </header>
    <main class="public-coupon-main">
      <div v-if="state === 'loading'" class="center-state" aria-busy="true">
        <p role="status">Проверяем купон…</p>
      </div>
      <div v-else-if="state === 'missing'" class="center-state">
        <h1>Купон не найден</h1>
        <p>Проверьте ссылку или обратитесь к специалисту, выдавшему купон.</p>
      </div>
      <div v-else-if="state === 'error'" class="center-state">
        <h1>Не удалось проверить купон</h1>
        <p role="alert">
          Статус сейчас неизвестен. Попробуйте еще раз или обратитесь к
          специалисту.
        </p>
        <button class="primary" type="button" @click="load">
          Проверить снова
        </button>
      </div>
      <template v-else-if="coupon">
        <article
          class="public-coupon-card"
          aria-labelledby="public-coupon-title"
        >
          <div class="public-card-top">
            <span class="card-label">REFERFLOW · ХОРОШИЕ РЕКОМЕНДАЦИИ</span>
            <h1 id="public-coupon-title">
              Купон № <strong>{{ coupon.code }}</strong>
            </h1>
          </div>
          <div class="public-card-offer">
            <p class="public-discount">{{ couponAmount(coupon) }}</p>
            <span>скидка</span>
          </div>
          <p
            class="public-status"
            :class="{ 'public-status-blocked': coupon.status !== 'available' }"
          >
            {{ publicStatus[coupon.status] }}
          </p>
          <p class="public-checked">Проверено: {{ checked }} (МСК)</p>
          <dl class="public-card-terms">
            <div v-for="[label, value] in couponTerms(coupon)" :key="label">
              <dt>{{ label }}</dt>
              <dd>{{ value }}</dd>
            </div>
          </dl>
          <section v-if="coupon.restrictions" class="public-restrictions">
            <h2>Ограничения по услугам</h2>
            <p>{{ coupon.restrictions }}</p>
          </section>
          <div class="public-card-bottom">
            <img
              class="public-qr"
              :src="qr"
              alt="QR-код со ссылкой на этот купон"
              width="176"
              height="176"
            />
            <p>
              Актуальные условия — по QR.<br />Покажите купон специалисту. Вход
              и регистрация не нужны.
            </p>
          </div>
          <p class="public-validity">{{ validityNote }}</p>
        </article>
        <section
          class="public-controls public-actions"
          aria-label="Действия с купоном"
        >
          <button
            class="primary"
            type="button"
            :disabled="exporting"
            @click="copyLink"
          >
            Скопировать ссылку
          </button>
          <button
            class="secondary"
            type="button"
            :disabled="exporting"
            @click="exportCard('image')"
          >
            Сохранить изображение
          </button>
          <button
            class="secondary"
            type="button"
            :disabled="exporting"
            @click="exportCard('print')"
          >
            Распечатать
          </button>
          <button
            class="secondary"
            type="button"
            :disabled="exporting"
            @click="load"
          >
            Обновить статус
          </button>
          <label v-if="fallbackCopy" class="public-copy-fallback"
            >Ссылка на купон<input
              ref="linkInput"
              :value="link"
              readonly
              @focus="linkInput?.select()"
          /></label>
          <p v-if="notice" role="status">{{ notice }}</p>
        </section>
      </template>
      <p v-if="actionError" class="error public-controls" role="alert">
        {{ actionError }}
      </p>
    </main>
  </div>
</template>
