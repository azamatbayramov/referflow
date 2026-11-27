<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch, useId } from 'vue'
class ClientFormError extends Error {}
type Client = { id: string; name: string; phone: string; archived?: boolean }
const props = defineProps<{ csrfToken: string }>()
const emit = defineEmits<{
  created: [client: Client]
  expired: []
  cancel: []
  pending: [pending: boolean]
}>()
const nameInput = ref<HTMLInputElement | null>(null)
const prefix = useId(),
  name = ref(''),
  phone = ref(''),
  busy = ref(false),
  error = ref(''),
  matches = ref<Client[]>([]),
  acknowledge = ref(false)
const frozen = ref<{ name: string; phone: string } | null>(null)
let key = crypto.randomUUID(),
  alive = true
let controller: AbortController | undefined
watch(
  () => busy.value || !!frozen.value,
  (value) => emit('pending', value),
  { immediate: true },
)
watch([name, phone], () => {
  acknowledge.value = false
  matches.value = []
})
async function request(path: string, data: unknown, signal: AbortSignal) {
  const response = await fetch(`/api/clients${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': props.csrfToken,
      ...(path === '' ? { 'Idempotency-Key': key } : {}),
    },
    body: JSON.stringify(data),
    signal,
  })
  if (response.status === 401) {
    emit('expired')
    throw new ClientFormError('Сеанс завершен. Войдите снова.')
  }
  return response
}
async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  controller = new AbortController()
  const signal = AbortSignal.any([
    controller.signal,
    AbortSignal.timeout(10000),
  ])
  try {
    if (!frozen.value) {
      const response = await request(
        '/search',
        { query: name.value, phone: phone.value, state: 'all' },
        signal,
      )
      if (!response.ok)
        throw new ClientFormError(
          'Не удалось проверить похожие карточки. Повторите попытку.',
        )
      const page = (await response.json()) as { items: Client[] }
      if (!alive) return
      matches.value = page.items
      if (matches.value.length && !acknowledge.value) {
        error.value = 'Проверьте похожие карточки ниже.'
        return
      }
      frozen.value = { name: name.value, phone: phone.value }
    }
    const response = await request('', frozen.value, signal)
    if (response.status === 400) {
      frozen.value = null
      key = crypto.randomUUID()
      throw new ClientFormError(
        'Проверьте имя и телефон: номер должен содержать 7–15 цифр.',
      )
    }
    if (!response.ok)
      throw new ClientFormError(
        'Не удалось сохранить клиента. Повторите отправку.',
      )
    const client = (await response.json()) as Client
    if (!alive) return
    frozen.value = null
    emit('created', client)
  } catch (e) {
    if (alive)
      error.value =
        e instanceof ClientFormError
          ? e.message
          : 'Нет связи. Повторите сохранение с теми же данными.'
  } finally {
    if (alive) busy.value = false
  }
}
function choose(client: Client) {
  if (!busy.value && !frozen.value && !client.archived) emit('created', client)
}
onMounted(() => nameInput.value?.focus())
onBeforeUnmount(() => {
  alive = false
  controller?.abort()
  emit('pending', false)
})
</script>
<template>
  <section class="quick-client" aria-label="Новый получатель скидки">
    <h4>Добавить клиента</h4>
    <p class="field-help">Имя обязательно. Телефон можно оставить пустым.</p>
    <form @submit.prevent="save">
      <fieldset :disabled="busy || !!frozen">
        <label :for="`${prefix}-name`">Имя / ФИО</label
        ><input
          :id="`${prefix}-name`"
          ref="nameInput"
          v-model="name"
          required
          maxlength="120"
          autocomplete="off"
        /><label :for="`${prefix}-phone`">Телефон (необязательно)</label
        ><input
          :id="`${prefix}-phone`"
          v-model="phone"
          type="tel"
          maxlength="40"
          autocomplete="off"
        />
      </fieldset>
      <div v-if="matches.length" class="quick-matches">
        <h5>Похожие карточки</h5>
        <ul>
          <li v-for="client in matches" :key="client.id">
            <button
              v-if="!client.archived"
              class="text-button"
              type="button"
              :disabled="busy || !!frozen"
              @click="choose(client)"
            >
              {{ client.name }} · {{ client.phone || 'Без телефона' }}</button
            ><span v-else>{{ client.name }} · В архиве</span>
          </li>
        </ul>
        <label class="check-label"
          ><input
            v-model="acknowledge"
            type="checkbox"
            :disabled="busy || !!frozen"
          />Это отдельный клиент, сохранить карточку</label
        >
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="frozen" class="field-help">
        Повтор сохранит одну карточку. Сначала завершите отправку.
      </p>
      <div class="workspace-actions">
        <button class="primary" :disabled="busy">
          {{
            busy
              ? 'Сохраняем…'
              : frozen
                ? 'Повторить сохранение клиента'
                : 'Сохранить и выбрать'
          }}</button><button
          class="secondary"
          type="button"
          :disabled="busy || !!frozen"
          @click="emit('cancel')"
        >
          Отмена
        </button>
      </div>
    </form>
  </section>
</template>
