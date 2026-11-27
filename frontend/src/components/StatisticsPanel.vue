<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

type Statistics = {
  as_of: string
  issued_coupons: number
  used_coupons: number
  active_redemptions: number
  unique_clients: number
  first_visit_clients: number
  canceled_redemptions: number
}
const emit = defineEmits<{
  expired: []
  coupons: [usage: 'all' | 'used' | 'unused']
}>()
const summary = ref<Statistics | null>(null),
  loading = ref(false),
  error = ref('')
let controller: AbortController | undefined
let alive = true,
  lastCheck = 0
const integer = new Intl.NumberFormat('ru-RU')
const percent = computed(() =>
  summary.value?.issued_coupons
    ? new Intl.NumberFormat('ru-RU', {
        style: 'percent',
        maximumFractionDigits: 1,
      }).format(summary.value.used_coupons / summary.value.issued_coupons)
    : '—',
)
async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  summary.value = null
  try {
    const response = await fetch('/api/coupons/statistics', {
      credentials: 'same-origin',
      cache: 'no-store',
      signal: AbortSignal.any([current.signal, AbortSignal.timeout(10000)]),
    })
    if (!alive || current.signal.aborted) return
    if (response.status === 401) {
      emit('expired')
      return
    }
    if (!response.ok) {
      error.value =
        response.status === 429
          ? 'Слишком много запросов. Повторите через минуту.'
          : 'Не удалось обновить статистику. Попробуйте еще раз.'
      return
    }
    const value = (await response.json()) as Statistics
    if (!alive || current.signal.aborted) return
    summary.value = value
    lastCheck = Date.now()
  } catch {
    if (alive && !current.signal.aborted)
      error.value = 'Нет связи с сервисом. Повторите попытку.'
  } finally {
    if (alive && !current.signal.aborted) loading.value = false
  }
}
function focus() {
  if (Date.now() - lastCheck > 3000) void load()
}
onMounted(() => {
  void load()
  window.addEventListener('focus', focus)
})
onBeforeUnmount(() => {
  alive = false
  controller?.abort()
  window.removeEventListener('focus', focus)
})
</script>

<template>
  <section
    class="statistics-section"
    aria-labelledby="statistics-title"
    :aria-busy="loading"
  >
    <div class="clients-title-row">
      <div>
        <p class="eyebrow"><span class="dot"></span> Результаты рекомендаций</p>
        <h2 id="statistics-title">Статистика пилота</h2>
      </div>
      <button class="secondary" type="button" :disabled="loading" @click="load">
        Обновить статистику
      </button>
    </div>
    <p class="muted">
      За все время. Только ваши купоны и записанные применения.
    </p>
    <p v-if="loading" role="status" class="muted">Считаем результаты…</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <template v-if="summary">
      <p class="field-help">
        Обновлено:
        {{
          new Date(summary.as_of).toLocaleString('ru-RU', {
            timeZone: 'Europe/Moscow',
          })
        }}
        (МСК)
      </p>
      <p v-if="!summary.issued_coupons" class="statistics-empty">
        Купонов пока нет. Создайте первый купон в разделе «Купоны».
      </p>
      <div class="statistics-grid">
        <article class="statistic-card">
          <h3>Выдано купонов</h3>
          <p class="statistic-value" data-testid="stat-issued">
            {{ integer.format(summary.issued_coupons) }}
          </p>
          <p>Включая истекшие и отозванные.</p>
          <button
            type="button"
            class="statistic-link"
            @click="emit('coupons', 'all')"
          >
            Посмотреть все купоны
          </button>
        </article>
        <article class="statistic-card">
          <h3>Купоны с применениями</h3>
          <p class="statistic-value" data-testid="stat-used">
            {{ integer.format(summary.used_coupons) }}
          </p>
          <p>Хотя бы одно неотмененное применение на купон.</p>
          <button
            type="button"
            class="statistic-link"
            @click="emit('coupons', 'used')"
          >
            Посмотреть купоны с применениями
          </button>
        </article>
        <article class="statistic-card">
          <h3>Неотмененные применения</h3>
          <p class="statistic-value" data-testid="stat-active">
            {{ integer.format(summary.active_redemptions) }}
          </p>
          <p>Многоразовый купон может дать несколько применений.</p>
        </article>
        <article class="statistic-card">
          <h3>Клиенты с купонами</h3>
          <p class="statistic-value" data-testid="stat-clients">
            {{ integer.format(summary.unique_clients) }}
          </p>
          <p>Клиенты с неотмененными применениями, каждый один раз.</p>
        </article>
        <article class="statistic-card">
          <h3>Доля использованных купонов</h3>
          <p class="statistic-value" data-testid="stat-share">{{ percent }}</p>
          <p>
            {{
              summary.issued_coupons
                ? `${integer.format(summary.used_coupons)} из ${integer.format(summary.issued_coupons)} выданных купонов`
                : 'Не рассчитывается, пока нет выданных купонов.'
            }}
          </p>
        </article>
        <article class="statistic-card statistic-first">
          <h3>Первое посещение подтверждено</h3>
          <p class="statistic-value" data-testid="stat-first">
            {{ integer.format(summary.first_visit_clients) }}
          </p>
          <p>
            Уникальные клиенты с неотмененным применением и ручным
            подтверждением специалиста. Это не независимая проверка новых
            посетителей.
          </p>
        </article>
      </div>
      <div class="statistics-notes">
        <p>
          <strong
            >Отмененных применений:
            <span data-testid="stat-canceled">{{
              integer.format(summary.canceled_redemptions)
            }}</span>.</strong
          >
          Они остаются в истории, но исключены из результатов выше. Отзыв или
          истечение купона не отменяет уже состоявшееся применение.
        </p>
        <p>
          Один клиент с разными купонами считается один раз. Отдельные карточки
          одного человека считаются отдельно — проверьте возможные дубликаты
          перед созданием клиента.
        </p>
        <p>
          Это учет купонов. Полная история посещений, возвращаемость, выручка и
          сэкономленные рубли здесь не рассчитываются.
        </p>
      </div>
    </template>
  </section>
</template>
