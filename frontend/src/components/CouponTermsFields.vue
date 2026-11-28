<script setup lang="ts" generic="T extends CouponTerms">
import { computed } from 'vue'
import type { CouponTerms } from '../coupon-terms'
const terms = defineModel<T>({ required: true })
defineProps<{ prefix: string }>()
const unlimitedTotal = computed({
  get: () => terms.value.total_limit === null,
  set: (value: boolean) => {
    terms.value.total_limit = value ? null : 1
  },
})
const unlimitedPersonal = computed({
  get: () => terms.value.per_client_limit === null,
  set: (value: boolean) => {
    terms.value.per_client_limit = value ? null : 1
  },
})
const timed = computed({
  get: () => terms.value.expires_on !== null,
  set: (value: boolean) => {
    terms.value.expires_on = value ? '' : null
  },
})
</script>
<template>
  <div class="coupon-term-fields">
    <h4 class="form-group-title">Скидка</h4>
    <label :for="`${prefix}-discount-type`">Вид скидки</label
    ><select :id="`${prefix}-discount-type`" v-model="terms.discount_type">
      <option value="percent">Процент</option>
      <option value="fixed">Фиксированная сумма, ₽</option>
    </select>
    <label :for="`${prefix}-discount-value`">{{
      terms.discount_type === 'percent' ? 'Скидка, %' : 'Скидка, ₽'
    }}</label><input
      :id="`${prefix}-discount-value`"
      v-model="terms.value"
      inputmode="decimal"
      enterkeyhint="next"
      required
      maxlength="14"
    />
    <p class="field-help">
      {{
        terms.discount_type === 'percent' ? 'От 0,01 до 100 %.' : 'От 0,01 ₽.'
      }}
      Не больше двух знаков после запятой.
    </p>
    <h4 class="form-group-title">Условия использования</h4>
    <label :for="`${prefix}-total-mode`">Общий лимит</label
    ><select :id="`${prefix}-total-mode`" v-model="unlimitedTotal">
      <option :value="true">Без ограничения</option>
      <option :value="false">Указать количество</option></select
    ><template v-if="!unlimitedTotal"
      ><label :for="`${prefix}-total-limit`">Всего использований</label
      ><input
        :id="`${prefix}-total-limit`"
        v-model="terms.total_limit"
        type="number"
        inputmode="numeric"
        min="1"
        max="2147483647"
        step="1"
        required
    /></template>
    <label :for="`${prefix}-personal-mode`">На одного клиента</label
    ><select :id="`${prefix}-personal-mode`" v-model="unlimitedPersonal">
      <option :value="false">Указать количество</option>
      <option :value="true">Без ограничения</option></select
    ><template v-if="!unlimitedPersonal"
      ><label :for="`${prefix}-personal-limit`">Использований на клиента</label
      ><input
        :id="`${prefix}-personal-limit`"
        v-model="terms.per_client_limit"
        type="number"
        inputmode="numeric"
        min="1"
        max="2147483647"
        step="1"
        required
    /></template>
    <label class="check-label"
      ><input v-model="terms.first_visit_only" type="checkbox" /><span
        >Только первое посещение специалиста</span
      ></label
    >
    <label class="check-label"
      ><input v-model="timed" type="checkbox" /><span
        >Указать срок действия</span
      ></label
    ><template v-if="timed"
      ><label :for="`${prefix}-coupon-expiry`">Последний день действия</label
      ><input
        :id="`${prefix}-coupon-expiry`"
        v-model="terms.expires_on"
        type="date"
        required
      />
      <p class="field-help">
        До конца этого дня по Москве (UTC+3). Прошедшая дата создаст уже
        истекший купон.
      </p></template
    >
    <label :for="`${prefix}-coupon-restrictions`"
      >Ограничения по услугам (необязательно)</label
    ><textarea
      :id="`${prefix}-coupon-restrictions`"
      v-model="terms.restrictions"
      maxlength="2000"
      rows="4"
    />
    <p class="field-help">
      Этот текст виден по публичной ссылке. Не указывайте в нем данные клиентов.
    </p>
  </div>
</template>
