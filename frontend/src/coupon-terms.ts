export type CouponTerms = {
  discount_type: 'percent' | 'fixed'
  value: string
  restrictions: string
  total_limit: number | null
  per_client_limit: number | null
  first_visit_only: boolean
  expires_on: string | null
}
export type CouponTemplate = CouponTerms & {
  id: string
  name: string
  archived: boolean
  version: number
  created_at: string
  updated_at: string
}
export function emptyTerms(): CouponTerms {
  return {
    discount_type: 'percent',
    value: '10',
    restrictions: '',
    total_limit: null,
    per_client_limit: 1,
    first_visit_only: false,
    expires_on: null,
  }
}
// Pick fields explicitly: names, metadata and client bindings never enter issuance.
export function copyTerms(t: CouponTerms): CouponTerms {
  return {
    discount_type: t.discount_type,
    value: t.value,
    restrictions: t.restrictions,
    total_limit: t.total_limit,
    per_client_limit: t.per_client_limit,
    first_visit_only: t.first_visit_only,
    expires_on: t.expires_on,
  }
}
