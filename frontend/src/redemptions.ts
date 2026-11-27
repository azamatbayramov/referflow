export type Redemption = {
  id: string
  coupon_id: string
  coupon_code: string
  client_id: string
  client_name: string
  author_id: string
  author_name: string
  discount_type: 'percent' | 'fixed'
  value: string
  single_coupon_confirmed: boolean
  first_visit_confirmed: boolean
  first_visit_required: boolean
  confirmed_at: string
  canceled_at: string | null
  canceled_by: string | null
  canceled_by_name: string | null
  cancel_reason: string | null
}
export class RedemptionError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}
const messages: Record<string, string> = {
  coupon_revoked: 'Купон отозван. Новое использование невозможно.',
  coupon_expired: 'Срок купона истек.',
  total_limit_reached: 'Общий лимит купона исчерпан.',
  personal_limit_reached: 'Этот клиент уже исчерпал свой лимит по купону.',
  client_archived: 'Карточка клиента в архиве. Сначала восстановите ее.',
  confirmation_required:
    'Подтвердите один купон на прием и первое посещение, если этого требуют условия.',
}
export async function redemptionRequest(
  path: string,
  csrf: string,
  body?: unknown,
  method = 'POST',
  key?: string,
  signal?: AbortSignal,
) {
  const response = await fetch(`/api/redemptions${path}`, {
    method,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf,
      ...(key ? { 'Idempotency-Key': key } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(10000)])
      : AbortSignal.timeout(10000),
  }).catch(() => {
    throw new Error('Нет связи с сервисом. Повторите действие.')
  })
  if (!response.ok) {
    const value = await response.json().catch(() => ({ status: '' }))
    throw new RedemptionError(
      messages[value.status] ??
        (response.status === 401
          ? 'Сеанс завершен. Войдите снова.'
          : response.status === 409
            ? 'Этот запрос уже сохранен с другими данными. Проверьте историю.'
            : response.status === 404
              ? 'Клиент, купон или операция недоступны.'
              : response.status === 400 && path.endsWith('/cancel')
                ? 'Укажите причину отмены (1–500 символов).'
                : response.status === 400
                  ? 'Проверьте клиента и подтверждения условий.'
                  : 'Не удалось выполнить действие. Повторите попытку.'),
      response.status,
    )
  }
  return response.json()
}
export function redemptionValue(r: { value: string; discount_type: string }) {
  return `${r.value.replace(/\.00$/, '').replace('.', ',')} ${r.discount_type === 'percent' ? '%' : '₽'}`
}
