import qrcode from 'qrcode-generator'

export type PublicCoupon = {
  code: string
  discount_type: 'percent' | 'fixed'
  value: string
  restrictions: string
  total_limit: number | null
  per_client_limit: number | null
  first_visit_only: boolean
  expires_on: string | null
  expires_at: string | null
  status: 'available' | 'expired' | 'revoked' | 'exhausted'
}
export const publicStatus = {
  available: 'Доступен',
  expired: 'Истек срок',
  revoked: 'Отозван',
  exhausted: 'Исчерпан общий лимит',
}
export const validityNote =
  'Статус может измениться. Перед использованием специалист проверит купон и условия для клиента. Копия или сканирование не резервируют скидку.'
export function couponLink(id: string) {
  return new URL(`/c/${id.toLowerCase()}`, window.location.origin).href
}
export function couponAmount(c: PublicCoupon) {
  return `${c.value.replace('.', ',')} ${c.discount_type === 'percent' ? '%' : '₽'}`
}
export function couponTerms(c: PublicCoupon): [string, string][] {
  return [
    [
      'Срок',
      c.expires_on
        ? `До ${c.expires_on.split('-').reverse().join('.')} включительно (МСК, UTC+3)`
        : 'Бессрочно',
    ],
    [
      'Посещения',
      c.first_visit_only
        ? 'Только первое посещение специалиста'
        : 'Любые посещения',
    ],
    [
      'Общий лимит использований',
      c.total_limit === null ? 'Без ограничения' : String(c.total_limit),
    ],
    [
      'Использований на клиента',
      c.per_client_limit === null
        ? 'Без ограничения'
        : String(c.per_client_limit),
    ],
    ['На один прием', 'Один купон'],
  ]
}
export function couponQR(link: string) {
  const qr = qrcode(0, 'M')
  qr.addData(link, 'Byte')
  qr.make()
  // Four quiet modules on every side. SVG stays sharp on paper and in PNG.
  const svg = qr.createSvgTag({ cellSize: 4, margin: 16, scalable: true })
  return `data:image/svg+xml;base64,${btoa(svg)}`
}

// Canvas export uses the same full terms as the HTML/print card. No external
// screenshot service, image proxy or third-party request receives the UUID.
export async function couponPNG(
  c: PublicCoupon,
  qrURL: string,
  checked: string,
) {
  const canvas = document.createElement('canvas')
  canvas.width = 1000
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('canvas unavailable')
  const rows: { text: string; size: number; bold: boolean; gap: number }[] = []
  function add(text: string, size = 28, bold = false, gap = 12) {
    ctx!.font = `${bold ? '600' : '400'} ${size}px sans-serif`
    for (const paragraph of text.split(/\r?\n/)) {
      let line = ''
      // Character wrapping also handles a 2000-character word without clipping.
      for (const char of paragraph) {
        if (line && ctx!.measureText(line + char).width > 872) {
          rows.push({ text: line, size, bold, gap: 0 })
          line = ''
        }
        line += char
      }
      rows.push({ text: line, size, bold, gap })
    }
  }
  add('referflow · купон', 28, true, 22)
  add(`№ ${c.code}`, 46, true)
  add(couponAmount(c), 82, true, 22)
  add(publicStatus[c.status], 32, true)
  add(`Проверено: ${checked} (МСК)`, 24, false, 22)
  for (const [label, value] of couponTerms(c)) add(`${label}: ${value}`)
  if (c.restrictions) {
    add('Ограничения по услугам', 28, true)
    add(c.restrictions)
  }
  add(validityNote, 24, false, 22)
  const contentHeight = rows.reduce(
    (sum, row) => sum + row.size * 1.4 + row.gap,
    0,
  )
  canvas.height = Math.ceil(128 + contentHeight + 350)
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  ctx.fillStyle = '#203d32'
  ctx.textBaseline = 'top'
  let y = 64
  for (const row of rows) {
    ctx.font = `${row.bold ? '600' : '400'} ${row.size}px sans-serif`
    ctx.fillText(row.text, 64, y)
    y += row.size * 1.4 + row.gap
  }
  const img = new Image()
  img.src = qrURL
  await img.decode()
  ctx.drawImage(img, 64, y, 280, 280)
  ctx.font = '24px sans-serif'
  ctx.fillText('Откройте актуальный купон по QR', 64, y + 300)
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error('export failed'))),
      'image/png',
    )
  })
}
