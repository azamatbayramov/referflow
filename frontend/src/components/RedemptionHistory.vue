<script setup lang="ts">
import { onBeforeUnmount, ref, watch, useId } from 'vue'
import {
  redemptionRequest,
  RedemptionError,
  redemptionValue,
  type Redemption,
} from '../redemptions'
const props = defineProps<{
  csrfToken: string
  clientId?: string
  couponId?: string
  refreshToken?: number
  title?: string
}>()
const emit = defineEmits<{
  expired: []
  changed: []
  pending: [pending: boolean]
}>()
const items = ref<Redemption[]>([]),
  hasMore = ref(false),
  offset = ref(0),
  loading = ref(false),
  busy = ref(false),
  error = ref(''),
  notice = ref('')
watch(busy, (value) => emit('pending', value))
const cancelId = ref(''),
  reason = ref('')
const prefix = useId()
let alive = true
let controller: AbortController | undefined
function failed(e: unknown) {
  if (e instanceof RedemptionError && e.status === 401) emit('expired')
  error.value = e instanceof Error ? e.message : 'Не удалось загрузить историю.'
}
async function load(reset = true) {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  const next = reset ? 0 : offset.value + 50
  try {
    const page = await redemptionRequest(
      '/search',
      props.csrfToken,
      {
        ...(props.clientId ? { client_id: props.clientId } : {}),
        ...(props.couponId ? { coupon_id: props.couponId } : {}),
        offset: next,
      },
      'POST',
      undefined,
      current.signal,
    )
    if (!alive || current.signal.aborted) return
    items.value = reset ? page.items : [...items.value, ...page.items]
    offset.value = next
    hasMore.value = page.has_more
  } catch (e) {
    if (alive && !current.signal.aborted) failed(e)
  } finally {
    if (alive && !current.signal.aborted) loading.value = false
  }
}
function startCancel(id: string) {
  cancelId.value = id
  reason.value = ''
  error.value = ''
  notice.value = ''
}
async function cancel() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await redemptionRequest(`/${cancelId.value}/cancel`, props.csrfToken, {
      reason: reason.value,
    })
    if (!alive) return
    cancelId.value = ''
    reason.value = ''
    notice.value = 'Использование отменено. Лимиты освобождены.'
    await load()
    emit('changed')
  } catch (e) {
    if (alive) failed(e)
  } finally {
    if (alive) busy.value = false
  }
}
watch(
  () => [props.clientId, props.couponId, props.refreshToken],
  () => {
    cancelId.value = ''
    reason.value = ''
    void load()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  alive = false
  controller?.abort()
  emit('pending', false)
})
</script>
<template>
  <section
    class="redemption-history"
    :aria-label="title || 'Использования купонов'"
  >
    <h4>{{ title || 'Использования купонов' }}</h4>
    <p class="field-help">
      Здесь только использования купонов. Посещения без купона не учитываются.
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="success" role="status">{{ notice }}</p>
    <p v-if="loading" class="muted" role="status">Загружаем историю…</p>
    <p v-else-if="!items.length" class="muted">
      Использований пока нет. Это не подтверждает первое посещение.
    </p>
    <ol class="redemption-rows">
      <li v-for="entry in items" :key="entry.id">
        <div class="redemption-heading">
          <strong
            >№ {{ entry.coupon_code }} · {{ redemptionValue(entry) }}</strong
          ><span>{{ entry.canceled_at ? 'Отменено' : 'Подтверждено' }}</span>
        </div>
        <p>
          {{ entry.client_name }} ·
          {{ new Date(entry.confirmed_at).toLocaleString('ru-RU') }}
        </p>
        <p class="muted">Подтвердил: {{ entry.author_name }}</p>
        <details class="confirmation-audit">
          <summary>Подтверждения специалиста</summary>
          <p>Один купон на фактический прием: подтверждено.</p>
          <p v-if="entry.first_visit_confirmed">
            Первое посещение: подтверждено специалистом.
          </p>
          <p v-else>Первое посещение не подтверждалось.</p>
        </details>
        <p v-if="entry.canceled_at" class="cancel-audit">
          Отменил: {{ entry.canceled_by_name }} ·
          {{ new Date(entry.canceled_at).toLocaleString('ru-RU') }}. Причина:
          {{ entry.cancel_reason }}
        </p>
        <form
          v-else-if="cancelId === entry.id"
          class="cancel-form"
          @submit.prevent="cancel"
        >
          <label :for="`${prefix}-cancel-${entry.id}`"
            >Причина отмены использования</label
          ><input
            :id="`${prefix}-cancel-${entry.id}`"
            v-model="reason"
            required
            maxlength="500"
            :disabled="busy"
          />
          <p class="field-help">
            Отмена сохранит историю и освободит лимиты. Отзыв и срок купона
            останутся в силе.
          </p>
          <div class="search-row">
            <button class="secondary" type="submit" :disabled="busy">
              {{ busy ? 'Отменяем…' : 'Подтвердить отмену' }}</button><button
              class="secondary"
              type="button"
              :disabled="busy"
              @click="cancelId = ''"
            >
              Назад
            </button>
          </div>
        </form>
        <button
          v-else
          type="button"
          class="secondary"
          :disabled="busy"
          @click="startCancel(entry.id)"
        >
          Отменить использование
        </button>
      </li>
    </ol>
    <button
      v-if="hasMore"
      class="secondary"
      type="button"
      :disabled="loading || busy"
      @click="load(false)"
    >
      Еще использования
    </button>
  </section>
</template>
