<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import CouponUseForm from './CouponUseForm.vue'
import CouponShare from './CouponShare.vue'
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
  status: 'available' | 'expired' | 'revoked' | 'exhausted'
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
const props = defineProps<{
  csrfToken: string
  initialUsage?: 'all' | 'used' | 'unused'
}>()
const usage = ref(props.initialUsage ?? 'all')
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
const tab = ref<'use' | 'share' | 'history'>('use')
const usePending = ref(false),
  historyPending = ref(false)
const searchInput = ref<HTMLInputElement | null>(null)
const listTitle = ref<HTMLElement | null>(null)
let listScroll = 0
const filterCount = computed(
  () => Number(state.value !== 'all') + Number(usage.value !== 'all'),
)
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
      {
        code: query.value,
        state: state.value,
        usage: usage.value,
        offset: next,
      },
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
  editor.value?.scrollIntoView({ block: 'start' })
}
async function create() {
  if (busy.value || frozen.value || usePending.value || historyPending.value)
    return
  listScroll = window.scrollY
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
  if (
    busy.value ||
    frozen.value ||
    ((usePending.value || historyPending.value) && selected.value?.id !== c.id)
  )
    return
  if (!selected.value) listScroll = window.scrollY
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const detail = await api(`/coupons/${c.id}`, undefined, 'GET')
    if (!alive) return
    selected.value = detail.coupon
    events.value = detail.events
    tab.value = 'use'
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
    tab.value = 'share'
    notice.value = 'Купон выдан.'
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
    const previousTab = tab.value
    await open(c)
    tab.value = previousTab
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
async function close() {
  if (busy.value || frozen.value || usePending.value || historyPending.value)
    return
  clientController?.abort()
  clientLoading.value = false
  form.value = false
  selected.value = null
  frozen.value = null
  error.value = ''
  notice.value = ''
  await nextTick()
  listTitle.value?.focus({ preventScroll: true })
  window.scrollTo({ top: listScroll })
}
async function showList(nextUsage: 'all' | 'used' | 'unused') {
  if (busy.value || frozen.value || usePending.value || historyPending.value)
    return
  await close()
  usage.value = nextUsage
  state.value = 'all'
  query.value = ''
  await load()
}
defineExpose({ showList })
function findCoupon() {
  searchInput.value?.focus()
  searchInput.value?.scrollIntoView({ block: 'center' })
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
  <section class="coupons-workspace" aria-label="Купоны">
    <div v-show="!form && !selected" class="coupon-list-screen">
      <div class="clients-title-row workspace-heading">
        <div>
          <h2 id="coupons-title" ref="listTitle" tabindex="-1">Купоны</h2>
          <p class="muted">Выдавайте купоны и отмечайте их использование.</p>
        </div>
        <div class="workspace-actions">
          <button class="secondary" type="button" @click="findCoupon">
            Использовать купон</button
          ><button
            class="primary"
            type="button"
            :disabled="busy"
            @click="create"
          >
            Выдать купон <span aria-hidden="true">+</span>
          </button>
        </div>
      </div>
      <form class="coupon-search-bar" role="search" @submit.prevent="load()">
        <label class="sr-only" for="coupon-search">Номер купона</label
        ><input
          id="coupon-search"
          ref="searchInput"
          v-model="query"
          inputmode="numeric"
          maxlength="20"
          placeholder="Номер купона"
        /><button class="primary" :disabled="loading">Найти</button>
      </form>
      <details class="coupon-filters">
        <summary>
          Фильтры<span v-if="filterCount"> · {{ filterCount }}</span>
        </summary>
        <div class="filter-fields">
          <div>
            <label for="coupon-state">Показать купоны</label
            ><select id="coupon-state" v-model="state" @change="load()">
              <option value="all">Все</option>
              <option value="available">Доступные</option>
              <option value="exhausted">Исчерпан общий лимит</option>
              <option value="expired">Истекшие</option>
              <option value="revoked">Отозванные</option>
            </select>
          </div>
          <div>
            <label for="coupon-usage">Применения купона</label
            ><select id="coupon-usage" v-model="usage" @change="load()">
              <option value="all">Все купоны</option>
              <option value="used">Есть неотмененные применения</option>
              <option value="unused">Нет неотмененных применений</option>
            </select>
          </div>
        </div>
      </details>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="loading" role="status" class="muted">Загружаем купоны…</p>
      <div v-else-if="!items.length" class="workspace-empty">
        <h3>
          {{
            query || filterCount
              ? 'Купоны не найдены'
              : 'Первый купон — начало рекомендации'
          }}
        </h3>
        <p>
          {{
            query || filterCount
              ? 'Проверьте номер или измените фильтры.'
              : 'Задайте скидку и передайте клиенту ссылку или бумажный купон.'
          }}
        </p>
      </div>
      <ul class="client-rows coupon-list">
        <li v-for="c in items" :key="c.id">
          <button type="button" :disabled="busy" @click="open(c)">
            <span class="coupon-list-main"
              ><strong>№ {{ c.code }} · {{ amount(c) }}</strong
              ><span class="muted">{{ expiry(c) }}</span></span
            ><span class="coupon-list-side"
              ><span class="status-chip" :class="`status-${c.status}`">{{
                labels[c.status]
              }}</span
              ><span class="client-state">Открыть →</span></span
            >
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
      class="coupon-detail-screen"
      aria-labelledby="coupon-editor-title"
    >
      <button
        class="text-button back-button"
        type="button"
        :disabled="busy || !!frozen || usePending || historyPending"
        @click="close"
      >
        ← Назад к купонам
      </button>
      <div class="editor-heading">
        <h2 id="coupon-editor-title" ref="editorTitle" tabindex="-1">
          {{ form ? 'Выдать купон' : `Купон № ${selected?.code}` }}
        </h2>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="notice" class="success" role="status">{{ notice }}</p>
      <div v-if="form" class="coupon-create-card">
        <form v-if="form" :aria-busy="busy" @submit.prevent="save">
          <fieldset class="coupon-fields" :disabled="busy || !!frozen">
            <p class="field-help">
              Номер присвоится автоматически. По умолчанию — один раз на
              клиента.
            </p>
            <h4 class="form-group-title">Скидка</h4>
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
            <h4 class="form-group-title">Условия использования</h4>
            <label for="total-mode">Общий лимит</label
            ><select id="total-mode" v-model="unlimitedTotal">
              <option :value="true">Без ограничения</option>
              <option :value="false">Указать количество</option></select
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
            <label for="personal-mode">На одного клиента</label
            ><select id="personal-mode" v-model="unlimitedPersonal">
              <option :value="false">Указать количество</option>
              <option :value="true">Без ограничения</option></select
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
            <p class="field-help">
              Этот текст виден по публичной ссылке. Не указывайте в нем данные
              клиентов.
            </p>
            <details class="coupon-referrer">
              <summary>Кто передаст купон (необязательно)</summary>
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
              <label for="referrer-client">Клиент, которому выдан купон</label
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
                  : 'Выдать купон'
            }}
          </button>
        </form>
      </div>
      <template v-else-if="selected">
        <div class="coupon-summary">
          <p class="coupon-amount">{{ amount(selected) }}</p>
          <span class="status-chip" :class="`status-${selected.status}`">{{
            labels[selected.status]
          }}</span>
          <p class="muted">
            {{ expiry(selected) }} ·
            {{
              selected.first_visit_only
                ? 'Только первое посещение'
                : 'Любые посещения'
            }}
          </p>
        </div>
        <div class="coupon-view-nav" aria-label="Действия с купоном">
          <button
            type="button"
            :aria-pressed="tab === 'use'"
            @click="tab = 'use'"
          >
            Использовать</button
          ><button
            type="button"
            :aria-pressed="tab === 'share'"
            @click="tab = 'share'"
          >
            Отправить</button
          ><button
            type="button"
            :aria-pressed="tab === 'history'"
            @click="tab = 'history'"
          >
            История
          </button>
        </div>
        <div v-show="tab === 'use'" class="coupon-action-card">
          <dl class="coupon-terms compact-terms">
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
          </dl>
          <CouponUseForm
            :key="selected.id"
            :coupon="selected"
            :csrf-token="csrfToken"
            :refresh-token="usageRevision"
            :blocked="historyPending"
            @expired="emit('expired')"
            @changed="refreshUsage"
            @pending="usePending = $event"
          />
        </div>
        <CouponShare
          v-if="tab === 'share'"
          :id="selected.id"
          :coupon="selected"
        />
        <div v-show="tab === 'history'" class="coupon-action-card">
          <RedemptionHistory
            :coupon-id="selected.id"
            :csrf-token="csrfToken"
            :refresh-token="usageRevision"
            title="История этого купона"
            @pending="historyPending = $event"
            @expired="emit('expired')"
            @changed="refreshUsage"
          />
          <details class="client-history">
            <summary>Выдача и отзыв</summary>
            <p v-if="selected.referrer_name">
              Кто передаст купон: {{ selected.referrer_name }}
            </p>
            <ol>
              <li v-for="event in events" :key="event.action">
                {{ event.action === 'created' ? 'Создан' : 'Отозван' }} ·
                {{ new Date(event.occurred_at).toLocaleString('ru-RU')
                }}<template v-if="event.reason"> · {{ event.reason }}</template>
              </li>
            </ol>
          </details>
          <p v-if="selected.revoked_at" class="error">
            Отозван {{ new Date(selected.revoked_at).toLocaleString('ru-RU') }}.
            Причина: {{ selected.revoke_reason }}
          </p>
          <details v-else class="coupon-revoke">
            <summary class="danger-text">Отозвать купон</summary>
            <form @submit.prevent="revoke">
              <p class="field-help">
                Отзыв окончательный. История сохранится, новые использования
                станут недоступны.
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
              ><button
                class="secondary danger-text"
                :disabled="busy || !confirmRevoke"
              >
                Отозвать купон
              </button>
            </form>
          </details>
        </div>
      </template>
    </section>
  </section>
</template>
