<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import CouponUseForm from './CouponUseForm.vue'
import { redemptionValue } from '../redemptions'
import type { CouponTerms } from '../coupon-terms'
type Coupon = CouponTerms & {
  id: string
  code: string
  status: string
  used_count: number
}
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const code = ref('')
const coupon = ref<Coupon | null>(null)
const busy = ref(false),
  pending = ref(false),
  error = ref(''),
  revision = ref(0)
let alive = true
const statusLabels: Record<string, string> = {
  available: 'Купон доступен',
  exhausted: 'Купон израсходован: исчерпан общий лимит',
  expired: 'Срок действия купона истек',
  revoked: 'Купон отозван',
}
async function request(path: string, body?: unknown) {
  const response = await fetch(`/api/coupons${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': props.csrfToken,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(10000),
  })
  if (response.status === 401) emit('expired')
  if (!response.ok)
    throw new Error(
      response.status === 429
        ? 'Слишком много запросов. Подождите минуту.'
        : 'Не удалось проверить купон. Повторите попытку.',
    )
  return response.json()
}
async function find() {
  if (busy.value || pending.value) return
  busy.value = true
  error.value = ''
  coupon.value = null
  revision.value = 0
  try {
    const page = await request('/search', {
      code: code.value.trim(),
      state: 'all',
      usage: 'all',
    })
    if (!alive) return
    coupon.value = page.items[0] ?? null
    if (!coupon.value)
      error.value = 'Купон с таким номером не найден. Проверьте номер.'
  } catch (e) {
    if (alive)
      error.value = e instanceof Error ? e.message : 'Нет связи с сервисом.'
  } finally {
    if (alive) busy.value = false
  }
}
async function refresh() {
  const id = coupon.value?.id
  if (!id) return
  try {
    const detail = await request(`/${id}`)
    if (alive && coupon.value?.id === id) {
      coupon.value = detail.coupon
      revision.value++
    }
  } catch {
    if (alive)
      error.value =
        'Не удалось обновить статус. Проверьте купон перед следующим использованием.'
  }
}
onBeforeUnmount(() => {
  alive = false
})
</script>
<template>
  <section class="redemption-workspace" aria-label="Применение купона">
    <div class="workspace-heading">
      <h2>Применить купон</h2>
      <p class="muted">Проверьте купон, затем выберите получателя скидки.</p>
    </div>
    <div class="scanner-placeholder" role="note">
      <svg viewBox="0 0 24 24" width="40" height="40" aria-hidden="true">
        <path
          d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5M7 12h10"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
        />
      </svg>
      <strong>Сканирование QR-кода</strong
      ><span>Камера появится здесь. Пока введите номер купона ниже.</span>
    </div>
    <form
      class="coupon-search-bar"
      role="search"
      aria-label="Проверка купона"
      @submit.prevent="find"
    >
      <label class="sr-only" for="redeem-code"
        >Номер купона для применения</label
      >
      <input
        id="redeem-code"
        v-model="code"
        inputmode="numeric"
        pattern="[0-9]{6}"
        minlength="6"
        maxlength="6"
        required
        placeholder="Номер купона · 6 цифр"
        :disabled="busy || pending"
      />
      <button class="primary" :disabled="busy || pending">
        {{ busy ? 'Проверяем…' : 'Проверить купон' }}
      </button>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template v-if="coupon">
      <div class="coupon-summary">
        <h3>Купон № {{ coupon.code }} · {{ redemptionValue(coupon) }}</h3>
        <p class="status-chip" :class="`status-${coupon.status}`" role="status">
          {{ statusLabels[coupon.status] }}
        </p>
        <p>
          {{
            coupon.expires_on
              ? `Действует до ${coupon.expires_on.split('-').reverse().join('.')} включительно (МСК)`
              : 'Без срока действия'
          }}
        </p>
        <p>
          {{
            coupon.first_visit_only
              ? 'Только первое посещение специалиста'
              : 'Любые посещения'
          }}
        </p>
        <p>
          {{
            coupon.total_limit === null
              ? 'Без общего лимита'
              : `Осталось использований: ${Math.max(0, coupon.total_limit - coupon.used_count)}`
          }}
          ·
          {{
            coupon.per_client_limit === null
              ? 'Без лимита на клиента'
              : `На клиента: ${coupon.per_client_limit}`
          }}
        </p>
        <p v-if="coupon.restrictions" class="template-restrictions">
          {{ coupon.restrictions }}
        </p>
      </div>
      <CouponUseForm
        v-show="coupon.status === 'available' || pending || revision > 0"
        :key="coupon.id"
        :coupon="coupon"
        :csrf-token="csrfToken"
        :refresh-token="revision"
        stepped
        id-prefix="redeem"
        @expired="emit('expired')"
        @pending="pending = $event"
        @changed="refresh"
      />
    </template>
  </section>
</template>
