<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import CouponTermsFields from './CouponTermsFields.vue'
import {
  copyTerms,
  emptyTerms,
  type CouponTerms,
  type CouponTemplate,
} from '../coupon-terms'
const props = defineProps<{ csrfToken: string; active: boolean }>()
const emit = defineEmits<{
  expired: []
  issue: [terms: CouponTerms, complete: (opened: boolean) => void]
}>()
type Draft = CouponTerms & { name: string; version: number; archived: boolean }
class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}
const items = ref<CouponTemplate[]>([])
const query = ref(''),
  state = ref('active'),
  offset = ref(0),
  hasMore = ref(false)
const loading = ref(false),
  busy = ref(false),
  editing = ref(false)
const selected = ref<CouponTemplate | null>(null)
const draft = ref<Draft>(empty())
const error = ref(''),
  notice = ref(''),
  stale = ref(false)
const frozen = ref<{
  path: string
  method: string
  body: Draft
  key: string
} | null>(null)
const title = ref<HTMLElement | null>(null)
let alive = true
let listController: AbortController | undefined
function empty(): Draft {
  return { ...emptyTerms(), name: '', version: 0, archived: false }
}
function fill(t: CouponTemplate): Draft {
  return {
    ...copyTerms(t),
    name: t.name,
    version: t.version,
    archived: t.archived,
  }
}
async function api(
  path: string,
  method: string,
  body?: unknown,
  key?: string,
  signal?: AbortSignal,
) {
  const response = await fetch(`/api/coupons/templates${path}`, {
    method,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': props.csrfToken,
      ...(key ? { 'Idempotency-Key': key } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(10000)])
      : AbortSignal.timeout(10000),
  })
  if (response.status === 401) emit('expired')
  if (!response.ok)
    throw new ApiError(
      response.status === 400
        ? 'Проверьте название, скидку, лимиты и срок.'
        : response.status === 409
          ? 'Шаблон изменился в другой вкладке. Загрузите актуальную версию перед редактированием.'
          : response.status === 404
            ? 'Шаблон недоступен. Обновите список.'
            : response.status === 429
              ? 'Слишком много запросов. Подождите минуту.'
              : response.status === 401
                ? 'Сеанс завершен. Войдите снова.'
                : 'Не удалось выполнить действие. Повторите попытку.',
      response.status,
    )
  return response.json()
}
function message(e: unknown) {
  return e instanceof ApiError
    ? e.message
    : 'Нет связи с сервисом. Повторите действие.'
}
async function load(reset = true) {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  loading.value = true
  const next = reset ? 0 : offset.value + 50
  try {
    const page = await api(
      '/search',
      'POST',
      { query: query.value, state: state.value, offset: next },
      undefined,
      controller.signal,
    )
    if (!alive || controller.signal.aborted) return
    items.value = reset ? page.items : [...items.value, ...page.items]
    hasMore.value = page.has_more
    offset.value = next
  } catch (e) {
    if (alive && !controller.signal.aborted) error.value = message(e)
  } finally {
    if (alive && !controller.signal.aborted) loading.value = false
  }
}
async function search() {
  error.value = ''
  await load()
}
async function focusTitle() {
  await nextTick()
  title.value?.focus()
  title.value?.scrollIntoView({ block: 'start' })
}
async function create() {
  if (busy.value || frozen.value) return
  selected.value = null
  draft.value = empty()
  editing.value = true
  stale.value = false
  error.value = notice.value = ''
  await focusTitle()
}
async function open(id: string) {
  if (busy.value || frozen.value) return
  busy.value = true
  error.value = notice.value = ''
  try {
    const t = (await api(`/${id}`, 'GET')) as CouponTemplate
    if (!alive) return
    selected.value = t
    draft.value = fill(t)
    editing.value = true
    stale.value = false
    await focusTitle()
  } catch (e) {
    if (alive) error.value = message(e)
  } finally {
    if (alive) busy.value = false
  }
}
async function save() {
  if (busy.value || stale.value) return
  busy.value = true
  error.value = notice.value = ''
  frozen.value ??= {
    path: selected.value ? `/${selected.value.id}` : '',
    method: selected.value ? 'PUT' : 'POST',
    body: { ...draft.value },
    key: crypto.randomUUID(),
  }
  try {
    const t = (await api(
      frozen.value.path,
      frozen.value.method,
      frozen.value.body,
      frozen.value.key,
    )) as CouponTemplate
    if (!alive) return
    frozen.value = null
    selected.value = t
    draft.value = fill(t)
    notice.value = 'Шаблон сохранен. Условия выданных купонов не изменились.'
    await load()
  } catch (e) {
    if (!alive) return
    if (e instanceof ApiError && [400, 404, 409].includes(e.status)) {
      frozen.value = null
      stale.value = e.status !== 400
    }
    error.value = message(e)
  } finally {
    if (alive) busy.value = false
  }
}
function close() {
  if (busy.value || frozen.value) return
  editing.value = false
  selected.value = null
  error.value = notice.value = ''
  void load()
}
async function issue(id: string) {
  if (busy.value || frozen.value) return
  busy.value = true
  error.value = notice.value = ''
  try {
    const t = (await api(`/${id}`, 'GET')) as CouponTemplate
    if (!alive) return
    if (t.archived) {
      error.value = 'Шаблон в архиве. Восстановите его перед выдачей.'
      await load()
      return
    }
    emit('issue', copyTerms(t), (opened) => {
      if (alive && !opened)
        error.value = 'Сначала завершите отправку в открытом купоне.'
    })
  } catch (e) {
    if (alive) error.value = message(e)
  } finally {
    if (alive) busy.value = false
  }
}
watch(
  () => props.active,
  (active) => {
    if (active && !editing.value) void load()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  alive = false
  listController?.abort()
})
</script>
<template>
  <section aria-label="Шаблоны купонов">
    <template v-if="!editing">
      <div class="clients-title-row workspace-heading">
        <div>
          <h2>Шаблоны купонов</h2>
          <p class="muted">
            Сохраните условия и выдавайте купоны без повторного ввода.
          </p>
        </div>
        <button class="primary" type="button" :disabled="busy" @click="create">
          Создать шаблон
        </button>
      </div>
      <form
        class="search-row template-search"
        role="search"
        @submit.prevent="search"
      >
        <div>
          <label for="template-search">Название шаблона</label
          ><input id="template-search" v-model="query" maxlength="120" />
        </div>
        <div>
          <label for="template-state">Показать шаблоны</label
          ><select id="template-state" v-model="state" @change="search">
            <option value="active">Активные</option>
            <option value="archived">В архиве</option>
            <option value="all">Все</option>
          </select>
        </div>
        <button class="secondary" :disabled="loading">Найти</button>
      </form>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="loading" role="status">Загружаем шаблоны…</p>
      <div v-else-if="!items.length" class="workspace-empty">
        <h3>Шаблоны не найдены</h3>
        <p>Создайте шаблон или измените поиск.</p>
      </div>
      <ul class="template-list">
        <li v-for="t in items" :key="t.id" class="coupon-action-card">
          <h3>{{ t.name }}</h3>
          <p>
            {{ t.value.replace(/\.00$/, '').replace('.', ',') }}
            {{ t.discount_type === 'percent' ? '%' : '₽' }} ·
            {{
              t.first_visit_only ? 'Только первое посещение' : 'Любые посещения'
            }}
          </p>
          <p class="muted">
            {{
              t.expires_on
                ? `До ${t.expires_on.split('-').reverse().join('.')} включительно (МСК)`
                : 'Бессрочно'
            }}{{ t.archived ? ' · В архиве' : '' }}
          </p>
          <div class="workspace-actions">
            <button
              v-if="!t.archived"
              class="primary"
              type="button"
              :disabled="busy"
              @click="issue(t.id)"
            >
              Выдать по шаблону
            </button>
            <button
              class="secondary"
              type="button"
              :disabled="busy"
              @click="open(t.id)"
            >
              Изменить
            </button>
          </div>
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
    </template>
    <div v-else class="coupon-detail-screen">
      <button
        class="text-button back-button"
        type="button"
        :disabled="busy || !!frozen"
        @click="close"
      >
        ← Назад к шаблонам
      </button>
      <h2 ref="title" tabindex="-1">
        {{ selected ? 'Изменить шаблон' : 'Создать шаблон' }}
      </h2>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="notice" class="success" role="status">{{ notice }}</p>
      <button
        v-if="stale && selected"
        class="secondary"
        type="button"
        :disabled="busy"
        @click="open(selected.id)"
      >
        Загрузить актуальную версию
      </button>
      <form class="coupon-create-card" :aria-busy="busy" @submit.prevent="save">
        <fieldset class="coupon-fields" :disabled="busy || !!frozen || stale">
          <label for="template-name">Название шаблона</label
          ><input
            id="template-name"
            v-model="draft.name"
            maxlength="120"
            required
          />
          <p class="field-help">
            Название видно только вам. При выдаче условия можно изменить для
            одного купона.
          </p>
          <CouponTermsFields v-model="draft" prefix="template" />
          <label v-if="selected" class="check-label"
            ><input v-model="draft.archived" type="checkbox" />Убрать шаблон в
            архив</label
          >
          <p v-if="selected" class="field-help">
            Архив убирает шаблон из выдачи. Выданные купоны продолжают
            действовать. Для восстановления снимите отметку и сохраните.
          </p>
        </fieldset>
        <p v-if="frozen" class="field-help">
          Повтор сохранит те же данные один раз. После перезагрузки сначала
          проверьте список шаблонов.
        </p>
        <button class="primary" :disabled="busy || stale">
          {{
            busy
              ? 'Сохраняем…'
              : frozen
                ? 'Повторить сохранение шаблона'
                : 'Сохранить шаблон'
          }}
        </button>
      </form>
    </div>
  </section>
</template>
