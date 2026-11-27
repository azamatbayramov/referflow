import { createApp } from 'vue'
import './style.css'

// These are document routes with ordinary links; no SPA router or shared session
// bootstrap is needed on a public coupon page.
const { default: App } = await (/^\/c(?:\/|$)/.test(window.location.pathname)
  ? import('./components/PublicCouponPage.vue')
  : import('./App.vue'))
createApp(App).mount('#app')
