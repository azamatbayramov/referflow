<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import RedemptionHistory from './RedemptionHistory.vue'
import QuickClientForm from './QuickClientForm.vue'
import {
  redemptionRequest,
  RedemptionError,
  redemptionValue,
} from '../redemptions'
type Coupon = {
  id: string
  code: string
  status: string
  first_visit_only: boolean
  restrictions: string
  discount_type: string
  value: string
}
type Client = { id: string; name: string; phone: string }
type Input = {
  coupon_id: string
  client_id: string
  single_coupon_confirmed: boolean
  first_visit_confirmed: boolean
}
const props = defineProps<{
  coupon: Coupon
  csrfToken: string
  refreshToken: number
  blocked?: boolean
  stepped?: boolean
  idPrefix?: string
}>()
const emit = defineEmits<{
  expired: []
  changed: []
  pending: [pending: boolean]
}>()
const prefix = computed(() => props.idPrefix ?? 'redemption')
const eligibility = ref(''),
  checking = ref(false)
const eligibilityLabels: Record<string, string> = {
  personal_limit:
    'Этот клиент уже использовал купон максимально допустимое число раз.',
  archived_client: 'Клиент находится в архиве. Выберите активного клиента.',
  revoked: 'Купон отозван.',
  expired: 'Срок действия купона истек.',
  total_limit: 'Общий лимит купона исчерпан.',
}
let eligibilityController: AbortController | undefined
async function checkEligibility() {
  eligibilityController?.abort()
  eligibility.value = ''
  checking.value = false
  if (!props.stepped || !clientId.value || frozen.value) return
  const current = new AbortController()
  eligibilityController = current
  checking.value = true
  try {
    const response = await fetch(
      `/api/coupons/${props.coupon.id}/eligibility`,
      {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': props.csrfToken,
        },
        body: JSON.stringify({ client_id: clientId.value }),
        signal: AbortSignal.any([current.signal, AbortSignal.timeout(10000)]),
      },
    )
    if (response.status === 401) emit('expired')
    if (!response.ok) throw new Error()
    const result = await response.json()
    if (alive && !current.signal.aborted) eligibility.value = result.status
  } catch {
    if (alive && !current.signal.aborted) eligibility.value = 'unavailable'
  } finally {
    if (alive && !current.signal.aborted) checking.value = false
  }
}
const addingClient = ref(false),
  quickPending = ref(false),
  recipientPending = ref(false)
function chooseCreated(client: Client) {
  clients.value = [client]
  clientId.value = client.id
  addingClient.value = false
  quickPending.value = false
  selectionChanged()
}
const query = ref(''),
  clients = ref<Client[]>([]),
  clientId = ref(''),
  loading = ref(false),
  busy = ref(false),
  single = ref(false),
  first = ref(false),
  error = ref(''),
  notice = ref(''),
  frozen = ref<Input | null>(null)
watch(
  () =>
    busy.value ||
    !!frozen.value ||
    quickPending.value ||
    recipientPending.value,
  (value) => emit('pending', value),
)
let key = crypto.randomUUID()
let alive = true
let controller: AbortController | undefined
function selectionChanged() {
  single.value = false
  first.value = false
  notice.value = ''
  error.value = ''
  key = crypto.randomUUID()
  void checkEligibility()
}
async function search() {
  if (busy.value || frozen.value || recipientPending.value || props.blocked)
    return
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  clientId.value = ''
  selectionChanged()
  try {
    const response = await fetch('/api/clients/search', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': props.csrfToken,
      },
      body: JSON.stringify({ query: query.value, state: 'active' }),
      signal: AbortSignal.any([current.signal, AbortSignal.timeout(10000)]),
    }).catch(() => {
      throw new Error('Нет связи с сервисом. Повторите поиск.')
    })
    if (response.status === 401) {
      emit('expired')
      return
    }
    if (!response.ok)
      throw new Error('Не удалось найти клиента. Повторите поиск.')
    const page = await response.json()
    if (alive && !current.signal.aborted) clients.value = page.items
  } catch (e) {
    if (alive && !current.signal.aborted)
      error.value = e instanceof Error ? e.message : 'Нет связи с сервисом.'
  } finally {
    if (alive && !current.signal.aborted) loading.value = false
  }
}
async function apply() {
  if (
    busy.value ||
    loading.value ||
    recipientPending.value ||
    props.blocked ||
    (!frozen.value && props.stepped && eligibility.value !== 'eligible')
  )
    return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    frozen.value ??= {
      coupon_id: props.coupon.id,
      client_id: clientId.value,
      single_coupon_confirmed: single.value,
      first_visit_confirmed: first.value,
    }
    const record = await redemptionRequest(
      '',
      props.csrfToken,
      frozen.value,
      'POST',
      key,
    )
    if (!alive) return
    frozen.value = null
    key = crypto.randomUUID()
    single.value = false
    first.value = false
    notice.value = record.canceled_at
      ? 'Это использование уже отменено. Новая операция не создана.'
      : 'Использование подтверждено.'
    emit('changed')
  } catch (e) {
    if (alive) {
      if (e instanceof RedemptionError && e.status === 401) emit('expired')
      if (e instanceof RedemptionError && [400, 404, 422].includes(e.status)) {
        frozen.value = null
        key = crypto.randomUUID()
        if (e.status === 422) emit('changed')
      }
      error.value =
        e instanceof Error ? e.message : 'Не удалось подтвердить использование.'
    }
  } finally {
    if (alive) busy.value = false
  }
}
watch(
  () => props.refreshToken,
  () => {
    if (!frozen.value) void checkEligibility()
  },
)
watch(
  () => props.coupon.id,
  () => {
    controller?.abort()
    clients.value = []
    clientId.value = ''
    frozen.value = null
    selectionChanged()
  },
)
onBeforeUnmount(() => {
  alive = false
  controller?.abort()
  eligibilityController?.abort()
  emit('pending', false)
})
</script>
<template>
  <section class="coupon-use" :aria-labelledby="`${prefix}-use-title`">
    <h4 :id="`${prefix}-use-title`">
      {{ stepped ? 'Получатель и подтверждения' : 'Использовать купон' }}
    </h4>
    <p class="field-help">Найдите клиента по имени или телефону.</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="success" role="status">{{ notice }}</p>
    <div v-show="!addingClient">
      <form
        class="client-search"
        role="search"
        aria-label="Поиск получателя скидки"
        @submit.prevent="search"
      >
        <label :for="`${prefix}-client-search`">Найти получателя скидки</label>
        <div class="search-row">
          <input
            :id="`${prefix}-client-search`"
            v-model="query"
            enterkeyhint="search"
            maxlength="120"
            :disabled="busy || !!frozen || recipientPending || blocked"
          /><button
            class="secondary"
            :disabled="
              busy || loading || !!frozen || recipientPending || blocked
            "
          >
            Найти
          </button>
        </div>
      </form>
      <button
        class="text-button add-client-link"
        type="button"
        :disabled="busy || !!frozen || recipientPending || blocked"
        @click="addingClient = true"
      >
        + Добавить клиента здесь
      </button>
    </div>
    <QuickClientForm
      v-if="addingClient"
      :csrf-token="csrfToken"
      @created="chooseCreated"
      @expired="emit('expired')"
      @pending="quickPending = $event"
      @cancel="addingClient = false"
    />
    <form v-show="!addingClient" @submit.prevent="apply">
      <fieldset
        class="coupon-fields"
        :disabled="busy || !!frozen || recipientPending || blocked"
      >
        <label :for="`${prefix}-client`">Получатель скидки</label
        ><select
          :id="`${prefix}-client`"
          v-model="clientId"
          required
          @change="selectionChanged"
        >
          <option value="">Выберите клиента</option>
          <option v-for="c in clients" :key="c.id" :value="c.id">
            {{ c.name }} · {{ c.phone || 'Без телефона' }}
          </option>
        </select>
        <p v-if="checking" role="status">Проверяем доступность для клиента…</p>
        <p
          v-else-if="stepped && eligibility && eligibility !== 'eligible'"
          class="error"
          role="alert"
        >
          {{
            eligibilityLabels[eligibility] ||
            'Не удалось проверить доступность для клиента.'
          }}
        </p>
        <button
          v-if="stepped && eligibility === 'unavailable'"
          class="secondary"
          type="button"
          @click="checkEligibility"
        >
          Повторить проверку
        </button>
        <div v-show="!stepped || eligibility === 'eligible' || !!frozen">
          <p class="use-value">
            {{ redemptionValue(coupon) }} · купон № {{ coupon.code }}
          </p>
          <p v-if="coupon.restrictions" class="use-restrictions">
            {{ coupon.restrictions }}
          </p>
          <p class="field-help">Проверьте условия и отметьте подтверждения.</p>
          <label class="check-label"
            ><input
              v-model="single"
              type="checkbox"
              :required="!stepped || eligibility === 'eligible'"
            /><span
              >Подтверждаю: на этот фактический прием применяется один
              купон</span
            ></label
          >
          <label v-if="coupon.first_visit_only" class="check-label"
            ><input
              v-model="first"
              type="checkbox"
              :required="!stepped || eligibility === 'eligible'"
            /><span
              >Подтверждаю: это первое посещение этого специалиста с учетом
              прежних приемов</span
            ></label
          >
        </div>
      </fieldset>
      <p v-if="frozen" class="field-help">
        Результат отправки мог сохраниться. Повторите тот же запрос; после
        перезагрузки сначала проверьте историю.
      </p>
      <p v-else-if="coupon.status !== 'available'" class="muted">
        Купон недоступен для нового использования.
      </p>
      <button
        v-show="!stepped || eligibility === 'eligible' || !!frozen"
        class="primary"
        :disabled="
          busy ||
          loading ||
          recipientPending ||
          blocked ||
          (!frozen && stepped && (checking || eligibility !== 'eligible')) ||
          (!frozen && coupon.status !== 'available')
        "
      >
        {{
          busy
            ? 'Подтверждаем…'
            : frozen
              ? 'Повторить подтверждение'
              : 'Подтвердить использование'
        }}
      </button>
    </form>
    <details v-if="clientId && !addingClient" class="recipient-history">
      <summary>История клиента</summary>
      <RedemptionHistory
        v-if="clientId"
        :client-id="clientId"
        :csrf-token="csrfToken"
        :refresh-token="refreshToken"
        title="История получателя скидки"
        @pending="recipientPending = $event"
        @expired="emit('expired')"
        @changed="emit('changed')"
      />
    </details>
  </section>
</template>
