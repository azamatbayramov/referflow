<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import RedemptionHistory from './RedemptionHistory.vue'

type Client = {
  id: string
  name: string
  phone: string
  archived: boolean
  version: number
  created_at: string
  updated_at: string
}
type Event = { version: number; action: string; occurred_at: string }
class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}
const props = defineProps<{ csrfToken: string; active: boolean }>()
const emit = defineEmits<{ expired: [] }>()
const items = ref<Client[]>([])
const query = ref('')
const state = ref('active')
const offset = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const form = ref(false)
const selected = ref<Client | null>(null)
const name = ref('')
const phone = ref('')
const busy = ref(false)
const matches = ref<Client[]>([])
const duplicatesChecked = ref(false)
const acknowledge = ref(false)
const events = ref<Event[]>([])
const historyRevision = ref(0)
const editor = ref<HTMLElement | null>(null)
const editorTitle = ref<HTMLElement | null>(null)
let creationKey = crypto.randomUUID()
const frozenCreate = ref<{ name: string; phone: string }>()
const historyPending = ref(false)
const listTitle = ref<HTMLElement | null>(null)
let listScroll = 0
let searchController: AbortController | undefined
let duplicateController: AbortController | undefined
let alive = true
let duplicateTimer: ReturnType<typeof setTimeout> | undefined

async function api(
  path: string,
  body?: unknown,
  method = 'POST',
  signal?: AbortSignal,
) {
  const response = await fetch(`/api/clients${path}`, {
    method,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': props.csrfToken,
      ...(path === '' ? { 'Idempotency-Key': creationKey } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(10000)])
      : AbortSignal.timeout(10000),
  }).catch(() => {
    throw new Error('Нет связи с сервисом. Проверьте соединение и повторите.')
  })
  if (response.status === 401) {
    emit('expired')
    throw new Error('Сеанс завершен. Войдите снова.')
  }
  if (!response.ok)
    throw new ApiError(
      response.status === 409
        ? 'Карточка уже изменена. Откройте ее заново и повторите правку.'
        : response.status === 400
          ? 'Проверьте имя и телефон: номер должен содержать 7–15 цифр.'
          : response.status === 404
            ? 'Карточка недоступна. Обновите список.'
            : 'Не удалось выполнить действие. Попробуйте снова.',
      response.status,
    )
  return response.json()
}
async function load(reset = true) {
  searchController?.abort()
  const controller = new AbortController()
  searchController = controller
  loading.value = true
  error.value = ''
  const requestedOffset = reset ? 0 : offset.value + 50
  try {
    const page = await api(
      '/search',
      { query: query.value, state: state.value, offset: requestedOffset },
      'POST',
      controller.signal,
    )
    if (!alive || controller.signal.aborted) return
    offset.value = requestedOffset
    items.value = reset ? page.items : [...items.value, ...page.items]
    hasMore.value = page.has_more
  } catch (e) {
    if (alive && !controller.signal.aborted)
      error.value =
        e instanceof Error ? e.message : 'Не удалось загрузить клиентов.'
  } finally {
    if (alive && !controller.signal.aborted) loading.value = false
  }
}
function more() {
  void load(false)
}
function resetForm() {
  duplicateController?.abort()
  clearTimeout(duplicateTimer)
  selected.value = null
  name.value = ''
  phone.value = ''
  matches.value = []
  events.value = []
  duplicatesChecked.value = false
  acknowledge.value = false
  frozenCreate.value = undefined
  creationKey = crypto.randomUUID()
}
async function focusEditor() {
  await nextTick()
  if (!alive) return
  editorTitle.value?.focus({ preventScroll: true })
  if (window.innerWidth <= 900) editor.value?.scrollIntoView({ block: 'start' })
}
async function create() {
  if (busy.value || frozenCreate.value || historyPending.value) return
  if (!form.value) listScroll = window.scrollY
  resetForm()
  error.value = ''
  notice.value = ''
  form.value = true
  await focusEditor()
}
async function open(client: Client) {
  if (busy.value || frozenCreate.value || historyPending.value) return
  if (!form.value) listScroll = window.scrollY
  duplicateController?.abort()
  clearTimeout(duplicateTimer)
  error.value = ''
  notice.value = ''
  busy.value = true
  try {
    const detail = await api(`/${client.id}`, undefined, 'GET')
    if (!alive) return
    selected.value = detail.client
    name.value = detail.client.name
    phone.value = detail.client.phone
    events.value = detail.events
    historyRevision.value++
    matches.value = []
    acknowledge.value = false
    duplicatesChecked.value = false
    frozenCreate.value = undefined
    form.value = true
    await focusEditor()
    void duplicates()
  } catch (e) {
    if (alive)
      error.value =
        e instanceof Error ? e.message : 'Не удалось открыть карточку.'
  } finally {
    if (alive) busy.value = false
  }
}
async function close() {
  if (busy.value || frozenCreate.value || historyPending.value) return
  duplicateController?.abort()
  clearTimeout(duplicateTimer)
  form.value = false
  resetForm()
  await nextTick()
  listTitle.value?.focus({ preventScroll: true })
  if (window.innerWidth <= 900) window.scrollTo({ top: listScroll })
}
function changed() {
  duplicateController?.abort()
  clearTimeout(duplicateTimer)
  duplicatesChecked.value = false
  acknowledge.value = false
  duplicateTimer = setTimeout(() => void duplicates(), 350)
}
async function duplicates() {
  duplicateController?.abort()
  const controller = new AbortController()
  duplicateController = controller
  if (!name.value.trim() && !phone.value.trim()) {
    matches.value = []
    return
  }
  try {
    const page = await api(
      '/search',
      { query: name.value, phone: phone.value, state: 'all' },
      'POST',
      controller.signal,
    )
    if (!alive || controller.signal.aborted) return
    matches.value = page.items.filter(
      (c: Client) => c.id !== selected.value?.id,
    )
    duplicatesChecked.value = true
  } catch (e) {
    if (alive && !controller.signal.aborted) {
      duplicatesChecked.value = false
      error.value =
        e instanceof Error
          ? e.message
          : 'Не удалось проверить похожие карточки.'
    }
  }
}
async function save() {
  if (busy.value) return
  error.value = ''
  notice.value = ''
  if (!name.value.trim()) {
    error.value = 'Укажите имя клиента.'
    return
  }
  busy.value = true
  try {
    if (!frozenCreate.value) {
      await duplicates()
      if (!duplicatesChecked.value) return
      if (matches.value.length && !acknowledge.value) {
        error.value = 'Проверьте похожие карточки и подтвердите сохранение.'
        return
      }
    }
    let client: Client
    if (selected.value)
      client = await api(
        `/${selected.value.id}`,
        {
          name: name.value,
          phone: phone.value,
          archived: selected.value.archived,
          version: selected.value.version,
        },
        'PUT',
      )
    else {
      frozenCreate.value ??= { name: name.value, phone: phone.value }
      client = await api('', frozenCreate.value)
    }
    if (!alive) return
    form.value = false
    resetForm()
    notice.value = 'Карточка сохранена.'
    await load()
    busy.value = false
    await open(client)
    notice.value = 'Карточка сохранена.'
  } catch (e) {
    if (e instanceof ApiError && e.status === 400 && !selected.value) {
      frozenCreate.value = undefined
      creationKey = crypto.randomUUID()
    }
    if (alive)
      error.value =
        e instanceof Error ? e.message : 'Не удалось сохранить карточку.'
  } finally {
    if (alive) busy.value = false
  }
}
async function archive() {
  if (!selected.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api(
      `/${selected.value.id}`,
      {
        name: selected.value.name,
        phone: selected.value.phone,
        archived: !selected.value.archived,
        version: selected.value.version,
      },
      'PUT',
    )
    if (!alive) return
    form.value = false
    resetForm()
    notice.value = 'Статус карточки изменен.'
    await load()
  } catch (e) {
    if (alive)
      error.value =
        e instanceof Error ? e.message : 'Не удалось изменить статус.'
  } finally {
    if (alive) busy.value = false
  }
}
const labels: Record<string, string> = {
  created: 'Карточка создана',
  updated: 'Контакты изменены',
  archived: 'В архив',
  restored: 'Возвращена из архива',
}
watch(
  () => props.active,
  (active) => {
    if (!active) return
    if (!form.value) void load()
    else if (selected.value && !busy.value && !historyPending.value)
      historyRevision.value++
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  alive = false
  searchController?.abort()
  duplicateController?.abort()
  clearTimeout(duplicateTimer)
})
</script>

<template>
  <section
    class="clients-section"
    :class="{ 'has-client-editor': form }"
    aria-labelledby="clients-title"
  >
    <div class="clients-title-row">
      <div>
        <p class="eyebrow"><span class="dot"></span> Ваши клиенты</p>
        <h2 id="clients-title" ref="listTitle" tabindex="-1">
          Карточки клиентов
        </h2>
      </div>
      <button
        class="primary"
        type="button"
        :disabled="busy || !!frozenCreate || historyPending"
        @click="create"
      >
        Добавить клиента <span aria-hidden="true">+</span>
      </button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" class="success" role="status">{{ notice }}</p>
    <div class="clients-grid">
      <div class="clients-list">
        <form class="client-search" role="search" @submit.prevent="load()">
          <label for="client-search">Поиск по имени или телефону</label>
          <div class="search-row">
            <input
              id="client-search"
              v-model="query"
              enterkeyhint="search"
              type="search"
              maxlength="120"
              autocomplete="off"
            /><button class="secondary" :disabled="loading" type="submit">
              Найти
            </button>
          </div>
          <label for="client-state">Показать</label
          ><select id="client-state" v-model="state" @change="load()">
            <option value="active">Активные</option>
            <option value="archived">Архив</option>
            <option value="all">Все клиенты</option>
          </select>
        </form>
        <p v-if="loading" class="muted" role="status">Загружаем клиентов…</p>
        <p v-else-if="!items.length" class="empty-clients">
          {{
            query
              ? 'Ничего не найдено. Проверьте имя или телефон.'
              : 'Здесь появятся карточки ваших клиентов.'
          }}
        </p>
        <ul class="client-rows">
          <li v-for="client in items" :key="client.id">
            <button
              type="button"
              :disabled="busy || !!frozenCreate || historyPending"
              :aria-pressed="selected?.id === client.id"
              @click="open(client)"
            >
              <span
                ><strong>{{ client.name }}</strong
                ><span class="muted">{{
                  client.phone || 'Телефон не указан'
                }}</span></span
              ><span class="client-state">{{
                client.archived ? 'В архиве' : 'Открыть'
              }}</span>
            </button>
          </li>
        </ul>
        <button
          v-if="hasMore"
          class="secondary"
          type="button"
          :disabled="loading"
          @click="more"
        >
          Показать еще
        </button>
      </div>
      <section
        v-if="form"
        ref="editor"
        class="client-editor"
        aria-labelledby="client-form-title"
      >
        <div class="editor-heading">
          <h3 id="client-form-title" ref="editorTitle" tabindex="-1">
            {{ selected ? 'Карточка клиента' : 'Новый клиент' }}
          </h3>
          <button
            class="secondary"
            type="button"
            :disabled="busy || !!frozenCreate || historyPending"
            @click="close"
          >
            Закрыть
          </button>
        </div>
        <p v-if="selected?.archived" class="muted">Карточка в архиве.</p>
        <form :aria-busy="busy" @submit.prevent="save">
          <label for="client-name">Имя / ФИО</label
          ><input
            id="client-name"
            v-model="name"
            required
            maxlength="120"
            autocomplete="off"
            :disabled="busy || !!frozenCreate"
            @input="changed"
          />
          <label for="client-phone"
            >Телефон <span class="muted">(необязательно)</span></label
          ><input
            id="client-phone"
            v-model="phone"
            type="tel"
            maxlength="40"
            autocomplete="off"
            :disabled="busy || !!frozenCreate"
            aria-describedby="phone-help"
            @input="changed"
          />
          <p id="phone-help" class="field-help">
            От 7 до 15 цифр. Общий номер у родственников допустим.
          </p>
          <div v-if="matches.length" class="duplicate-box">
            <h4>Похожие карточки</h4>
            <p>Убедитесь, что этот человек еще не добавлен.</p>
            <ul>
              <li v-for="match in matches" :key="match.id">
                <button
                  type="button"
                  class="text-button"
                  :disabled="busy"
                  @click="open(match)"
                >
                  {{ match.name }} · {{ match.phone || 'Без телефона'
                  }}{{ match.archived ? ' · В архиве' : '' }}
                </button>
              </li>
            </ul>
            <label class="check-label"
              ><input
                v-model="acknowledge"
                type="checkbox"
                :disabled="busy"
              />Это отдельный клиент, сохранить карточку</label
            >
          </div>
          <p v-if="frozenCreate" class="field-help">
            Если ответ не пришел, повторите сохранение. Данные отправки
            сохранены до закрытия формы.
          </p>
          <button class="primary" type="submit" :disabled="busy">
            {{
              busy
                ? 'Сохраняем…'
                : frozenCreate
                  ? 'Повторить сохранение'
                  : 'Сохранить карточку'
            }}
          </button>
        </form>
        <template v-if="selected"
          ><button
            class="secondary archive-button"
            type="button"
            :disabled="busy"
            @click="archive"
          >
            {{ selected.archived ? 'Вернуть из архива' : 'В архив' }}
          </button>
          <p class="field-help">
            Архивирование сохраняет карточку и ее историю.
          </p>
          <details class="client-history">
            <summary>История изменений</summary>
            <ol>
              <li v-for="event in events" :key="event.version">
                {{ labels[event.action] }} ·
                {{ new Date(event.occurred_at).toLocaleString('ru-RU') }}
              </li>
            </ol>
          </details>
          <RedemptionHistory
            :client-id="selected.id"
            :csrf-token="csrfToken"
            title="Использования купонов клиента"
            :refresh-token="historyRevision"
            @expired="emit('expired')"
            @pending="historyPending = $event"
          />
        </template>
      </section>
      <div v-else class="client-editor editor-empty">
        <p class="eyebrow">КЛИЕНТЫ ПОД РУКОЙ</p>
        <h3>Найдите карточку<br />или добавьте новую.</h3>
        <p class="muted">
          Сначала проверьте поиск — так история человека останется в одной
          карточке.
        </p>
      </div>
    </div>
  </section>
</template>
