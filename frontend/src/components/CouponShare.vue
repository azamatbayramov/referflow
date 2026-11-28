<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import {
  couponLink,
  couponQR,
  couponPNG,
  couponTerms,
  type PublicCoupon,
} from '../public-coupon'
const props = defineProps<{ id: string; coupon: PublicCoupon }>()
const link = computed(() => couponLink(props.id))
const qr = computed(() => couponQR(link.value))
const notice = ref(''),
  error = ref(''),
  exporting = ref(false),
  fallback = ref(false)
const linkInput = ref<HTMLInputElement | null>(null)
async function copy() {
  notice.value = ''
  error.value = ''
  try {
    await navigator.clipboard.writeText(link.value)
    notice.value = 'Ссылка скопирована.'
  } catch {
    fallback.value = true
    await nextTick()
    linkInput.value?.focus()
    linkInput.value?.select()
  }
}
async function download() {
  if (exporting.value) return
  exporting.value = true
  error.value = ''
  try {
    const response = await fetch(`/api/public/coupons/${props.id}`, {
      cache: 'no-store',
      credentials: 'omit',
      referrerPolicy: 'no-referrer',
      signal: AbortSignal.timeout(10000),
    })
    if (!response.ok) throw new Error('unavailable')
    const coupon = (await response.json()) as PublicCoupon
    const checked = new Date().toLocaleString('ru-RU', {
      timeZone: 'Europe/Moscow',
    })
    const blob = await couponPNG(coupon, qr.value, checked)
    const url = URL.createObjectURL(blob),
      a = document.createElement('a')
    a.href = url
    a.download = `referflow-${coupon.code}.png`
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    notice.value = 'Изображение сохранено.'
  } catch {
    error.value =
      'Не удалось проверить купон и сохранить изображение. Повторите попытку.'
  } finally {
    exporting.value = false
  }
}
</script>
<template>
  <section class="share-card" aria-label="Передать купон">
    <div class="share-qr">
      <img
        :src="qr"
        alt="QR-код со ссылкой на купон"
        width="176"
        height="176"
      />
      <p class="field-help">Покажите QR клиенту<br />или отправьте ссылку.</p>
    </div>
    <div class="share-content">
      <h3>Купон готов к передаче</h3>
      <p class="muted">Клиенту не нужны регистрация и приложение.</p>
      <div class="share-actions">
        <button class="primary" type="button" @click="copy">
          Скопировать ссылку</button
        ><button
          class="secondary"
          type="button"
          :disabled="exporting"
          @click="download"
        >
          Сохранить изображение</button
        ><a :href="`/c/${id}`" target="_blank" rel="noreferrer"
          >Открыть карточку для клиента</a
        >
      </div>
      <p class="field-help">
        Печать доступна в карточке для клиента. Номер {{ coupon.code }} можно
        переписать на бумагу.
      </p>
      <p v-if="notice" class="success" role="status">{{ notice }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <label v-if="fallback"
        >Ссылка на купон<input ref="linkInput" :value="link" readonly
      /></label>
    </div>
    <div class="share-terms">
      <h4>Условия для клиента</h4>
      <dl class="coupon-terms">
        <div v-for="[label, value] in couponTerms(coupon)" :key="label">
          <dt>{{ label }}</dt>
          <dd>{{ value }}</dd>
        </div>
      </dl>
      <p v-if="coupon.restrictions" class="preserve-text">
        {{ coupon.restrictions }}
      </p>
    </div>
  </section>
</template>
