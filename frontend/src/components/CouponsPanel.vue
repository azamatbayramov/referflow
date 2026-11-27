<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import CouponUseForm from './CouponUseForm.vue'
import RedemptionHistory from './RedemptionHistory.vue'

type Input = {
  discount_type: 'percent' | 'fixed'
  value: string
  restrictions: string
  total_limit: number | null
  per_client_limit: number | null
  first_visit_only: boolean
  expires_on: string | null
  referrer_client_id: string | null
}
type Coupon = Input & {
  id: string
  code: string
  used_count: number
  status: string
  expires_at: string | null
  revoked_at: string | null
  revoke_reason: string | null
  created_at: string
  referrer_name: string | null
}
type Event = { action: string; reason: string | null; occurred_at: string }
type Client = { id: string; name: string; phone: string }
class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{ expired: [] }>()
const items = ref<Coupon[]>([]),
  selected = ref<Coupon | null>(null),
  events = ref<Event[]>([])
const query = ref(''),
  state = ref('all'),
  offset = ref(0),
  hasMore = ref(false),
  loading = ref(false),
  busy = ref(false)
const usageRevision = ref(0)
const form = ref(false),
  error = ref(''),
  notice = ref(''),
  reason = ref(''),
  confirmRevoke = ref(false)
const draft = ref<Input>(empty()),
  unlimitedTotal = ref(true),
  unlimitedPersonal = ref(false),
  timed = ref(false)
const clientQuery = ref(''),
  clients = ref<Client[]>([]),
  clientLoading = ref(false)
const frozen = ref<Input | null>(null)
let key = crypto.randomUUID()
let alive = true
let listController: AbortController | undefined
let clientController: AbortController | undefined
const editor = ref<HTMLElement | null>(null),
  editorTitle = ref<HTMLElement | null>(null)
const labels: Record<string, string> = {
  available: 'Доступен',
  expired: 'Истек срок',
  revoked: 'Отозван',
  exhausted: 'Исчерпан лимит',
}
function empty(): Input {
  return {
    discount_type: 'percent',
    value: '10',
    restrictions: '',
    total_limit: null,
    per_client_limit: 1,
    first_visit_only: false,
    expires_on: null,
    referrer_client_id: null,
  }
}
async function api(
  path: string,
  body?: unknown,
  method = 'POST',
  signal?: AbortSignal,
) {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': props.csrfToken,
      ...(path === '/coupons' ? { 'Idempotency-Key': key } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(10000)])
      : AbortSignal.timeout(10000),
  }).catch(() => {
    throw new Error('Нет связи с сервисом. Повторите действие.')
  })
  if (response.status === 401) {
    emit('expired')
    throw new Error('Сеанс завершен. Войдите снова.')
  }
  if (!response.ok) {
    throw new ApiError(
      response.status === 400
        ? path.endsWith('/revoke')
          ? 'Укажите причину отзыва (1–500 символов).'
          : 'Проверьте номер, номинал, лимиты и срок купона.'
        : response.status === 429
          ? 'Слишком много запросов. Подождите минуту.'
          : response.status === 404
            ? 'Купон недоступен. Обновите список.'
            : response.status === 409
              ? 'Данные повторной отправки различаются. Проверьте поиск перед новой выдачей.'
              : 'Не удалось выполнить действие. Повторите попытку.',
      response.status,
    )
  }
  return response.json()
}
async function load(reset = true) {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  loading.value = true
  error.value = ''
  const next = reset ? 0 : offset.value + 50
  try {
    const page = await api(
      '/coupons/search',
      { code: query.value, state: state.value, offset: next },
      'POST',
      controller.signal,
    )
    if (!alive || controller.signal.aborted) return
    items.value = reset ? page.items : [...items.value, ...page.items]
    offset.value = next
    hasMore.value = page.has_more
  } catch (e) {
    if (alive && !controller.signal.aborted)
      error.value =
        e instanceof Error ? e.message : 'Не удалось загрузить купоны.'
  } finally {
    if (alive && !controller.signal.aborted) loading.value = false
  }
}
async function focusEditor() {
  await nextTick()
  if (!alive) return
  editorTitle.value?.focus({ preventScroll: true })
  if (window.innerWidth <= 900) editor.value?.scrollIntoView({ block: 'start' })
}
async function create() {
  if (busy.value) return
  clientController?.abort()
  clientLoading.value = false
  draft.value = empty()
  unlimitedTotal.value = true
  unlimitedPersonal.value = false
  timed.value = false
  frozen.value = null
  key = crypto.randomUUID()
  selected.value = null
  form.value = true
  error.value = ''
  notice.value = ''
  clients.value = []
  clientQuery.value = ''
  await focusEditor()
}
async function open(c: Coupon) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const detail = await api(`/coupons/${c.id}`, undefined, 'GET')
    if (!alive) return
    selected.value = detail.coupon
    events.value = detail.events
    form.value = false
    reason.value = ''
    confirmRevoke.value = false
    await focusEditor()
  } catch (e) {
    if (alive)
      error.value = e instanceof Error ? e.message : 'Не удалось открыть купон.'
  } finally {
    if (alive) busy.value = false
  }
}
async function findClients() {
  clientController?.abort()
  const controller = new AbortController()
  clientController = controller
  draft.value.referrer_client_id = null
  clientLoading.value = true
  error.value = ''
  try {
    const page = await api(
      '/clients/search',
      {
        query: clientQuery.value,
        state: 'active',
      },
      'POST',
      controller.signal,
    )
    if (alive && !controller.signal.aborted) clients.value = page.items
  } catch (e) {
    if (alive && !controller.signal.aborted)
      error.value = e instanceof Error ? e.message : 'Не удалось найти клиента.'
  } finally {
    if (alive && !controller.signal.aborted) clientLoading.value = false
  }
}
async function save() {
  if (busy.value || clientLoading.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    frozen.value ??= {
      ...draft.value,
      total_limit: unlimitedTotal.value
        ? null
        : Number(draft.value.total_limit),
      per_client_limit: unlimitedPersonal.value
        ? null
        : Number(draft.value.per_client_limit),
      expires_on: timed.value ? draft.value.expires_on : null,
    }
    const c = await api('/coupons', frozen.value)
    if (!alive) return
    form.value = false
    frozen.value = null
    await load()
    busy.value = false
    await open(c)
    notice.value = 'Купон создан.'
  } catch (e) {
    if (alive) {
      if (e instanceof ApiError && e.status === 400) {
        frozen.value = null
        key = crypto.randomUUID()
      }
      error.value = e instanceof Error ? e.message : 'Не удалось создать купон.'
    }
  } finally {
    if (alive) busy.value = false
  }
}
async function revoke() {
  if (!selected.value || busy.value || !confirmRevoke.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const c = await api(`/coupons/${selected.value.id}/revoke`, {
      reason: reason.value,
    })
    if (!alive) return
    selected.value = c
    await load()
    busy.value = false
    await open(c)
    notice.value = 'Купон окончательно отозван.'
  } catch (e) {
    if (alive)
      error.value =
        e instanceof Error ? e.message : 'Не удалось отозвать купон.'
  } finally {
    if (alive) busy.value = false
  }
}
async function refreshUsage() {
  if (!selected.value) return
  const id = selected.value.id
  try {
    const detail = await api(`/coupons/${id}`, undefined, 'GET')
    if (!alive || selected.value?.id !== id) return
    selected.value = detail.coupon
    events.value = detail.events
    usageRevision.value++
    await load()
  } catch (e) {
    if (alive)
      error.value =
        e instanceof Error
          ? e.message
          : 'Обновите купон, чтобы проверить счетчик.'
  }
}
function close() {
  if (busy.value) return
  clientController?.abort()
  clientLoading.value = false
  form.value = false
  selected.value = null
  frozen.value = null
  error.value = ''
}
function amount(c: Coupon) {
  return `${c.value.replace(/\.00$/, '').replace('.', ',')} ${c.discount_type === 'percent' ? '%' : '₽'}`
}
function expiry(c: Coupon) {
  if (!c.expires_on) return 'Бессрочно'
  const [y, m, d] = c.expires_on.split('-')
  return `До ${d}.${m}.${y} включительно (МСК, UTC+3)`
}
onMounted(() => void load())
onBeforeUnmount(() => {
  alive = false
  listController?.abort()
  clientController?.abort()
})
</script>

<template>
  <section class="clients-section" aria-labelledby="coupons-title">
    <div class="clients-title-row">
      <div>
        <p class="eyebrow"><span class="dot"></span> Рекомендации</p>
        <h2 id="coupons-title">Купоны</h2>
      </div>
      <button class="primary" :disabled="busy" type="button" @click="create">
        Создать купон <span aria-hidden="true">+</span>
      </button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="success" role="status">{{ notice }}</p>
    <div class="clients-grid">
      <div class="clients-list">
        <form class="client-search" role="search" @submit.prevent="load()">
          <label for="coupon-search">Номер купона</label>
          <div class="search-row">
            <input
              id="coupon-search"
              v-model="query"
              inputmode="numeric"
              maxlength="20"
              placeholder="Например, 123456"
            /><button class="secondary" :disabled="loading">Найти</button>
          </div>
          <label for="coupon-state">Показать купоны</label
          ><select id="coupon-state" v-model="state" @change="load()">
            <option value="all">Все</option>
            <option value="available">Доступные</option>
            <option value="expired">Истекшие</option>
            <option value="revoked">Отозванные</option>
          </select>
        </form>
        <p v-if="loading" role="status" class="muted">Загружаем купоны…</p>
        <p v-else-if="!items.length" class="empty-clients">
          Купоны не найдены.
        </p>
        <ul class="client-rows">
          <li v-for="c in items" :key="c.id">
            <button
              type="button"
              :disabled="busy"
              :aria-pressed="selected?.id === c.id"
              @click="open(c)"
            >
              <span
                ><strong>№ {{ c.code }} · {{ amount(c) }}</strong
                ><span class="muted">{{ labels[c.status] }}</span></span
              ><span class="client-state">Открыть</span>
            </button>
          </li>
        </ul>
        <button
          v-if="hasMore"
          class="secondary"
          type="button"
          :disabled="loading"
          @click="load(false)"
        >
          Показать еще
        </button>
      </div>
      <section
        v-if="form || selected"
        ref="editor"
        class="client-editor"
        aria-labelledby="coupon-editor-title"
      >
        <div class="editor-heading">
          <h3 id="coupon-editor-title" ref="editorTitle" tabindex="-1">
            {{ form ? 'Новый купон' : `Купон № ${selected?.code}` }}
          </h3>
          <button
            class="secondary"
            type="button"
            :disabled="busy"
            @click="close"
          >
            Закрыть
          </button>
        </div>
        <form v-if="form" :aria-busy="busy" @submit.prevent="save">
          <fieldset class="coupon-fields" :disabled="busy || !!frozen">
            <p class="muted">
              Номер купона присваивается автоматически после сохранения.
            </p>
            <label for="discount-type">Вид скидки</label
            ><select id="discount-type" v-model="draft.discount_type">
              <option value="percent">Процент</option>
              <option value="fixed">Фиксированная сумма, ₽</option>
            </select>
            <label for="discount-value">{{
              draft.discount_type === 'percent' ? 'Скидка, %' : 'Скидка, ₽'
            }}</label><input
              id="discount-value"
              v-model="draft.value"
              inputmode="decimal"
              required
              maxlength="14"
            />
            <p class="field-help">
              {{
                draft.discount_type === 'percent'
                  ? 'От 0,01 до 100 %.'
                  : 'От 0,01 ₽.'
              }}
              Не больше двух знаков после запятой.
            </p>
            <label class="check-label"
              ><input v-model="unlimitedTotal" type="checkbox" />Без общего
              лимита</label
            ><template v-if="!unlimitedTotal"
              ><label for="total-limit">Всего использований</label
              ><input
                id="total-limit"
                v-model="draft.total_limit"
                type="number"
                min="1"
                max="2147483647"
                step="1"
                required
            /></template>
            <label class="check-label"
              ><input v-model="unlimitedPersonal" type="checkbox" />Без лимита
              на клиента</label
            ><template v-if="!unlimitedPersonal"
              ><label for="personal-limit">Использований на клиента</label
              ><input
                id="personal-limit"
                v-model="draft.per_client_limit"
                type="number"
                min="1"
                max="2147483647"
                step="1"
                required
            /></template>
            <label class="check-label"
              ><input v-model="draft.first_visit_only" type="checkbox" />Только
              первое посещение специалиста</label
            >
            <label class="check-label"
              ><input v-model="timed" type="checkbox" />Указать срок
              действия</label
            ><template v-if="timed"
              ><label for="coupon-expiry">Последний день действия</label
              ><input
                id="coupon-expiry"
                v-model="draft.expires_on"
                type="date"
                required
              />
              <p class="field-help">
                До конца этого дня по Москве (UTC+3). Прошедшая дата создаст уже
                истекший купон.
              </p></template
            >
            <label for="coupon-restrictions"
              >Ограничения по услугам (необязательно)</label
            ><textarea
              id="coupon-restrictions"
              v-model="draft.restrictions"
              maxlength="2000"
              rows="4"
            />
            <details class="coupon-referrer">
              <summary>Кому выдан (необязательно)</summary>
              <p class="field-help">
                Клиент, который передаст купон другому человеку. Это не привязка
                получателя скидки.
              </p>
              <label for="referrer-search">Найти клиента</label>
              <div class="search-row">
                <input
                  id="referrer-search"
                  v-model="clientQuery"
                  maxlength="120"
                /><button
                  class="secondary"
                  type="button"
                  :disabled="clientLoading"
                  @click="findClients"
                >
                  Искать
                </button>
              </div>
              <label for="referrer-client">Кому выдан купон</label
              ><select id="referrer-client" v-model="draft.referrer_client_id">
                <option :value="null">Не указан</option>
                <option v-for="c in clients" :key="c.id" :value="c.id">
                  {{ c.name }} · {{ c.phone || 'Без телефона' }}
                </option>
              </select>
            </details>
          </fieldset>
          <p v-if="frozen" class="field-help">
            Повторная отправка сохранит один купон. После закрытия формы сначала
            проверьте поиск.
          </p>
          <button class="primary" :disabled="busy || clientLoading">
            {{
              busy
                ? 'Сохраняем…'
                : frozen
                  ? 'Повторить создание'
                  : 'Сохранить купон'
            }}
          </button>
        </form>
        <template v-else-if="selected">
          <p class="coupon-amount">{{ amount(selected) }}</p>
          <p class="session-badge coupon-status">
            {{ labels[selected.status] }}
          </p>
          <dl class="coupon-terms">
            <div>
              <dt>Срок</dt>
              <dd>{{ expiry(selected) }}</dd>
            </div>
            <div>
              <dt>Посещения</dt>
              <dd>
                {{
                  selected.first_visit_only
                    ? 'Только первое посещение'
                    : 'Любые посещения'
                }}
              </dd>
            </div>
            <div>
              <dt>Общий лимит</dt>
              <dd>{{ selected.total_limit ?? 'Без ограничения' }}</dd>
            </div>
            <div>
              <dt>Лимит на клиента</dt>
              <dd>{{ selected.per_client_limit ?? 'Без ограничения' }}</dd>
            </div>
            <div>
              <dt>Использовано</dt>
              <dd>{{ selected.used_count }}</dd>
            </div>
            <div v-if="selected.referrer_name">
              <dt>Кому выдан</dt>
              <dd>{{ selected.referrer_name }}</dd>
            </div>
          </dl>
          <section v-if="selected.restrictions" class="coupon-conditions">
            <h4>Ограничения по услугам</h4>
            <p>{{ selected.restrictions }}</p>
          </section>
          <CouponUseForm
            :key="selected.id"
            :coupon="selected"
            :csrf-token="csrfToken"
            :refresh-token="usageRevision"
            @expired="emit('expired')"
            @changed="refreshUsage"
          />
          <RedemptionHistory
            :coupon-id="selected.id"
            :csrf-token="csrfToken"
            :refresh-token="usageRevision"
            title="История этого купона"
            @expired="emit('expired')"
            @changed="refreshUsage"
          />
          <p v-if="selected.revoked_at" class="error">
            Отозван {{ new Date(selected.revoked_at).toLocaleString('ru-RU') }}.
            Причина: {{ selected.revoke_reason }}
          </p>
          <form v-else class="coupon-revoke" @submit.prevent="revoke">
            <h4>Отзыв купона</h4>
            <p class="field-help">
              После отзыва новые использования запрещены. Вернуть купон в
              действие нельзя.
            </p>
            <label for="revoke-reason">Причина отзыва</label
            ><input
              id="revoke-reason"
              v-model="reason"
              required
              maxlength="500"
              :disabled="busy"
            /><label class="check-label"
              ><input
                v-model="confirmRevoke"
                type="checkbox"
                required
                :disabled="busy"
              />Понимаю, что отзыв окончательный</label
            ><button class="secondary" :disabled="busy || !confirmRevoke">
              Отозвать купон
            </button>
          </form>
          <details class="client-history">
            <summary>История купона</summary>
            <ol>
              <li v-for="event in events" :key="event.action">
                {{ event.action === 'created' ? 'Создан' : 'Отозван' }} ·
                {{ new Date(event.occurred_at).toLocaleString('ru-RU')
                }}<template v-if="event.reason"> · {{ event.reason }}</template>
              </li>
            </ol>
          </details>
        </template>
      </section>
      <div v-else class="client-editor editor-empty">
        <p class="eyebrow">ХОРОШИЕ РЕКОМЕНДАЦИИ</p>
        <h3>Найдите купон<br />или создайте новый.</h3>
        <p class="muted">
          Условия сохраняются при выдаче. Для другой скидки создайте новый
          купон.
        </p>
      </div>
    </div>
  </section>
</template>
