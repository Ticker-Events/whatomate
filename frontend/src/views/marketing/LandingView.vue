<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useIntersectionObserver } from '@vueuse/core'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Bot,
  Check,
  Clock,
  MessageSquare,
  Moon,
  PhoneOff,
  ShoppingBag,
  TrendingUp,
  Users,
  Zap,
  ArrowRight,
  MessageCircleWarning,
} from 'lucide-vue-next'

const SUPPORT_PHONE = '917795937091'
const SUPPORT_EMAIL = 'hello@tiqr.events'

type AiPlanId = 'base' | 'advanced' | 'pro'

interface AiPlan {
  id: AiPlanId
  name: string
  monthlyFee: number
  freeOrders: number
  perOrder: number
  highlight?: boolean
  blurb: string
}

const aiPlans: AiPlan[] = [
  {
    id: 'base',
    name: 'AI Base',
    monthlyFee: 2500,
    freeOrders: 300,
    perOrder: 15,
    blurb: 'AI agent for growing stores that want reliable WhatsApp automation.',
  },
  {
    id: 'advanced',
    name: 'AI Advanced',
    monthlyFee: 5000,
    freeOrders: 600,
    perOrder: 10,
    highlight: true,
    blurb: 'Lower per-order cost and more included volume for busy teams.',
  },
  {
    id: 'pro',
    name: 'AI Pro',
    monthlyFee: 10000,
    freeOrders: 1200,
    perOrder: 7.5,
    blurb: 'Best unit economics at scale for high-order WhatsApp operations.',
  },
]

const pains = [
  {
    icon: PhoneOff,
    title: 'Jammed by phone calls?',
    body: 'Every ring steals time from the floor. Peak hours turn into missed opportunities.',
  },
  {
    icon: MessageCircleWarning,
    title: 'Cannot respond to every WhatsApp?',
    body: 'Chats pile up while you serve customers. Slow replies mean lost trust — and lost sales.',
  },
  {
    icon: ShoppingBag,
    title: 'Order drop-offs',
    body: 'Customers browse, ask, then disappear when nobody answers in time.',
  },
]

const capabilities = [
  {
    icon: Bot,
    title: 'WhatsApp AI agent',
    body: 'Engage customers in real time with personalized recommendations and order handling on the channel they already use.',
  },
  {
    icon: ShoppingBag,
    title: 'Order management',
    body: 'Streamline processing, track status, and answer inquiries without juggling apps or notebooks.',
  },
  {
    icon: Moon,
    title: 'Take orders while you sleep',
    body: 'Your AI agent keeps selling after hours — so overnight messages become morning revenue.',
  },
]

const benefits = [
  {
    icon: TrendingUp,
    title: 'Increase revenue',
    body: 'Cut drop-offs and capture orders outside business hours.',
  },
  {
    icon: Zap,
    title: 'Enhanced efficiency',
    body: 'Automate replies and streamline order management so your team focuses on high-value work.',
  },
  {
    icon: Users,
    title: 'Better customer experience',
    body: 'Delight buyers with personalized service, convenient ordering, and timely updates.',
  },
]

const faqs = [
  {
    q: 'Can this work for any type of store?',
    a: 'Yes. TiQR Agent works for bakeries, restaurants, retail, clinics, services, and more — anything that takes orders or inquiries on WhatsApp.',
  },
  {
    q: 'Can you build custom flows for my business?',
    a: 'Yes. We can design custom WhatsApp flows for your catalog, booking, payments, and handoffs. Contact us and we’ll scope what you need.',
    contact: true,
  },
  {
    q: 'What’s included in the hero demo?',
    a: 'The demo shows a real order path: welcome → buy → pickup/delivery → collections → product cards → cart → checkout → order confirmation — the same journey your customers can take on WhatsApp.',
  },
  {
    q: 'How does AI pricing work?',
    a: 'Each AI plan has a monthly fee plus free orders. Extra orders bill at that plan’s per-order rate. Use the calculator above to estimate your monthly cost. Payment gateway charges are billed as actuals.',
  },
  {
    q: 'What’s the difference between Basic and AI plans?',
    a: 'Basic (₹999/month) is WhatsApp Business essentials only — inbox, templates, messaging — with no AI agent. AI Base / Advanced / Pro add the WhatsApp AI agent that takes orders and replies for you.',
  },
  {
    q: 'Is pricing per store?',
    a: 'Yes. Charges are per store per month. If you run multiple outlets, each store is billed separately.',
  },
]

/** Demo transcript from tiqr-agent-demo.mp4 — Palooda Payyannur → Bakery Bengaluru */
type DemoNode =
  | { kind: 'date'; label: string }
  | { kind: 'out'; text: string; quote?: string }
  | { kind: 'in'; html: string; buttons?: string[] }
  | { kind: 'products'; items: { name: string; price: string; desc: string; img: string }[] }
  | { kind: 'status'; html: string; payNow?: boolean }

const demoProducts = [
  {
    name: 'KIWI',
    price: '₹9',
    desc: 'KIWI cake from Bakery Bengaluru.',
    img: '/demo/cake-kiwi.jpg',
  },
  {
    name: 'MANGO',
    price: '₹9',
    desc: 'MANGO cake from Bakery Bengaluru.',
    img: '/demo/cake-mango.jpg',
  },
  {
    name: 'STRAWBERRY',
    price: '₹9',
    desc: 'STRAWBERRY cake from Bakery Bengaluru.',
    img: '/demo/cake-strawberry.jpg',
  },
]

const demoNodes: DemoNode[] = [
  { kind: 'date', label: '16 October 2026' },
  { kind: 'out', text: 'Hi' },
  {
    kind: 'in',
    html: 'Welcome to <strong>Bakery Bengaluru</strong>! We specialize in Bengaluru cakes and a delightful range of desserts. Enjoy convenient pickup or delivery. Choose below to buy, check your order, or chat with us.<br/><br/>What would you like to do?',
    buttons: ['Buy products', 'Check order status', 'Talk to staff'],
  },
  { kind: 'out', text: 'Buy products', quote: 'What would you like to do?' },
  {
    kind: 'in',
    html: 'How would you like to receive your order?',
    buttons: ['Store pickup', 'Delivery'],
  },
  { kind: 'out', text: 'Store pickup', quote: 'How would you like to receive your order?' },
  {
    kind: 'in',
    html: '<strong>Our collections</strong><br/>Choose a collection below and we\'ll show you the items in it. Tap Browse to continue.',
    buttons: ['Browse'],
  },
  {
    kind: 'out',
    text: 'Fruit Cakes',
    quote: "Fruit Cakes selection from Bakery Bengaluru's cake menu.",
  },
  {
    kind: 'in',
    html: 'Here is what\'s available in <strong>Fruit Cakes</strong>. Tap <strong>Add to cart</strong> on the item you\'d like.',
  },
  { kind: 'products', items: demoProducts },
  {
    kind: 'in',
    html: 'How many <strong>KIWI</strong> would you like? Reply with a whole number of 1 or more, for example 1.',
  },
  { kind: 'out', text: '1' },
  {
    kind: 'in',
    html: '<strong>KIWI</strong> has been added to your cart.<br/><br/><strong>Your cart:</strong><br/>1. KIWI(KIWI) x1 — ₹9.00<br/><strong>Subtotal:</strong> ₹9.00<br/><br/>You now have <strong>1</strong> items in your cart. Add something else, edit your cart, or check out when you\'re ready.',
    buttons: ['Checkout', 'Add more', 'Edit cart'],
  },
  { kind: 'out', text: 'Checkout' },
  {
    kind: 'in',
    html: '<strong>Your cart:</strong><br/>1. KIWI(KIWI) x1 — ₹9.00<br/>Subtotal: ₹9.00<br/><br/>Please confirm these items before we continue checkout.',
    buttons: ['Confirm items', 'Edit cart', 'Add more'],
  },
  { kind: 'out', text: 'Confirm items', quote: 'Please confirm these items before we continue checkout.' },
  {
    kind: 'status',
    payNow: true,
    html: 'Order placed! Your order number is <strong class="text-emerald-400">PD-261016-000511</strong>.<br/>Delivery fee: Free<br/>Total: ₹9.00',
  },
  {
    kind: 'status',
    html: '<strong>Booking Update.</strong><br/><br/>This is an update about your booking request placed with <strong>Bakery Bengaluru</strong><br/><br/>Order ID: <strong>PD-261016-000511</strong><br/>Latest Status: <strong>Confirmed</strong><br/><br/>Next Steps:<br/><em>Great news! Your order has been confirmed and is now being packed.</em><br/><br/>Regards,<br/><strong>Bakery Bengaluru</strong><br/><span class="text-[10px] text-white/40">discovered on tiqr.store</span>',
  },
]

const orderPresets = [500, 1200, 3000]
const orderCount = ref(1200)
const heroReady = ref(false)
const demoStep = ref(0)
const chatScrollEl = ref<HTMLElement | null>(null)

const planTotals = computed(() =>
  aiPlans.map((plan) => {
    const extra = Math.max(0, orderCount.value - plan.freeOrders)
    const total = plan.monthlyFee + extra * plan.perOrder
    return { ...plan, extra, total }
  })
)

const cheapestPlanId = computed(() => {
  let best = planTotals.value[0]
  for (const row of planTotals.value) {
    if (row.total < best.total) best = row
  }
  return best.id
})

const lead = reactive({
  name: '',
  business: '',
  phone: '',
  plan: 'AI Advanced' as string,
})

const formatInr = (n: number) =>
  new Intl.NumberFormat('en-IN', {
    style: 'currency',
    currency: 'INR',
    maximumFractionDigits: n % 1 === 0 ? 0 : 2,
  }).format(n)

function openWhatsApp(message: string) {
  window.open(`https://wa.me/${SUPPORT_PHONE}?text=${encodeURIComponent(message)}`, '_blank', 'noopener,noreferrer')
}

function submitLead() {
  if (!lead.name.trim() || !lead.business.trim()) return
  const lines = [
    `Hi TiQR Agent team — I'd like to talk about ${lead.plan}.`,
    `Name: ${lead.name.trim()}`,
    `Business: ${lead.business.trim()}`,
  ]
  if (lead.phone.trim()) lines.push(`Phone: ${lead.phone.trim()}`)
  lines.push(`Estimated monthly orders: ${orderCount.value}`)
  openWhatsApp(lines.join('\n'))
}

function scrollToLead() {
  document.getElementById('lead')?.scrollIntoView({ behavior: 'smooth' })
}

function talkAboutPlan(planName: string) {
  lead.plan = planName
  scrollToLead()
}

function useReveal() {
  const el = ref<HTMLElement | null>(null)
  const shown = ref(false)
  useIntersectionObserver(
    el,
    ([entry]) => {
      if (entry?.isIntersecting) shown.value = true
    },
    { threshold: 0.15 }
  )
  return { el, shown }
}

const { el: painsEl, shown: painsShown } = useReveal()
const { el: capsEl, shown: capsShown } = useReveal()
const { el: benefitsEl, shown: benefitsShown } = useReveal()
const { el: pricingEl, shown: pricingShown } = useReveal()

let demoTimer: ReturnType<typeof setTimeout> | undefined

onMounted(() => {
  requestAnimationFrame(() => {
    heroReady.value = true
  })

  const schedule = (fn: () => void, ms: number) => {
    demoTimer = window.setTimeout(fn, ms)
  }

  const advance = () => {
    if (demoStep.value < demoNodes.length) {
      demoStep.value += 1
      requestAnimationFrame(() => {
        if (chatScrollEl.value) {
          chatScrollEl.value.scrollTop = chatScrollEl.value.scrollHeight
        }
      })
      const node = demoNodes[demoStep.value - 1]
      const delay =
        node?.kind === 'products' ? 2200 : node?.kind === 'status' ? 1600 : node?.kind === 'in' ? 1400 : 900
      schedule(advance, delay)
    } else {
      schedule(() => {
        demoStep.value = 0
        schedule(advance, 600)
      }, 2800)
    }
  }
  schedule(advance, 500)
})

onUnmounted(() => {
  if (demoTimer) window.clearTimeout(demoTimer)
})
</script>

<template>
  <div class="landing min-h-screen bg-[#07080a] text-white light:bg-slate-50 light:text-slate-900">
    <header class="sticky top-0 z-40 border-b border-white/[0.06] bg-[#07080a]/80 backdrop-blur-md light:border-slate-200 light:bg-white/80">
      <div class="mx-auto flex h-16 max-w-6xl items-center justify-between px-4 sm:px-6">
        <a href="#top" class="flex items-center gap-2.5 font-semibold tracking-tight">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 shadow-lg shadow-emerald-500/20">
            <MessageSquare class="h-5 w-5 text-white" />
          </span>
          <span>TiQR Agent</span>
        </a>
        <nav class="hidden items-center gap-6 text-sm text-white/60 light:text-slate-600 md:flex">
          <a href="#problems" class="hover:text-white light:hover:text-slate-900">Problems</a>
          <a href="#solution" class="hover:text-white light:hover:text-slate-900">Solution</a>
          <a href="#pricing" class="hover:text-white light:hover:text-slate-900">Pricing</a>
          <a href="#faq" class="hover:text-white light:hover:text-slate-900">FAQ</a>
          <a href="#lead" class="hover:text-white light:hover:text-slate-900">Contact</a>
        </nav>
        <div class="flex items-center gap-2">
          <RouterLink
            to="/login"
            class="inline-flex h-9 items-center rounded-md px-3 text-sm text-white/80 hover:bg-white/[0.06] light:text-slate-700 light:hover:bg-slate-100"
          >
            Sign in
          </RouterLink>
          <a
            href="#lead"
            class="inline-flex h-9 items-center rounded-md bg-emerald-600 px-3 text-sm font-medium text-white hover:bg-emerald-500"
          >
            Talk to us
          </a>
        </div>
      </div>
    </header>

    <main id="top">
      <!-- Hero -->
      <section class="relative overflow-hidden border-b border-white/[0.06] light:border-slate-200">
        <div
          class="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top,_rgba(16,185,129,0.2),_transparent_55%),radial-gradient(ellipse_at_bottom_right,_rgba(13,148,136,0.12),_transparent_40%)]"
        />
        <div class="relative mx-auto grid max-w-6xl gap-12 px-4 py-16 sm:px-6 lg:grid-cols-[1.05fr_0.95fr] lg:items-center lg:py-24">
          <div
            class="transition-all duration-700 ease-out"
            :class="heroReady ? 'translate-y-0 opacity-100' : 'translate-y-6 opacity-0'"
          >
            <p class="mb-4 text-sm font-medium uppercase tracking-[0.22em] text-emerald-400/90">
              TiQR Agent
            </p>
            <h1 class="max-w-xl text-4xl font-semibold tracking-tight text-white light:text-slate-900 sm:text-5xl lg:text-[3.25rem] lg:leading-[1.1]">
              AI-powered WhatsApp orders
            </h1>
            <p class="mt-5 max-w-lg text-lg text-white/60 light:text-slate-600">
              Engage customers in real time with an AI WhatsApp agent that recommends products, manages orders, and replies when your team cannot.
            </p>
            <div class="mt-8 flex flex-wrap gap-3">
              <a
                href="#lead"
                class="cta-pulse inline-flex h-11 items-center rounded-md bg-emerald-600 px-6 text-sm font-medium text-white hover:bg-emerald-500"
              >
                Stop missing WhatsApp orders
                <ArrowRight class="ml-2 h-4 w-4" />
              </a>
              <a
                href="#pricing"
                class="inline-flex h-11 items-center rounded-md border border-white/15 bg-white/[0.03] px-6 text-sm font-medium text-white hover:bg-white/[0.06] light:border-slate-300 light:bg-white light:text-slate-900 light:hover:bg-slate-50"
              >
                See AI pricing
              </a>
            </div>
          </div>

          <!-- Demo chat from tiqr-agent-demo.mp4 -->
          <div
            class="transition-all delay-150 duration-700 ease-out"
            :class="heroReady ? 'translate-y-0 opacity-100' : 'translate-y-8 opacity-0'"
          >
            <div class="relative mx-auto w-full max-w-[340px] overflow-hidden rounded-[1.75rem] border border-white/[0.12] bg-[#0b1410] shadow-2xl shadow-emerald-950/50">
              <div class="flex items-center gap-3 border-b border-white/[0.06] bg-[#075e54] px-3 py-2.5">
                <span class="flex h-9 w-9 items-center justify-center rounded-full bg-white/15">
                  <Bot class="h-5 w-5 text-white" />
                </span>
                <div class="min-w-0 flex-1">
                  <p class="truncate text-sm font-semibold text-white">Bakery Bengaluru</p>
                  <p class="flex items-center gap-1 text-[11px] text-emerald-100/90">
                    <span class="online-dot h-1.5 w-1.5 rounded-full bg-emerald-300" />
                    TiQR Agent · online
                  </p>
                </div>
              </div>

              <div
                ref="chatScrollEl"
                class="demo-chat flex h-[420px] flex-col gap-2.5 overflow-y-auto px-3 py-3"
              >
                <template v-for="(node, idx) in demoNodes" :key="idx">
                  <div v-if="demoStep > idx" class="chat-bubble flex w-full flex-col gap-2">
                    <div
                      v-if="node.kind === 'date'"
                      class="mx-auto rounded-full bg-white/10 px-3 py-0.5 text-[10px] text-white/60"
                    >
                      {{ node.label }}
                    </div>

                    <div
                      v-else-if="node.kind === 'out'"
                      class="ml-auto max-w-[88%] rounded-2xl rounded-br-md bg-[#005c4b] px-3 py-2 text-[13px] leading-snug text-white"
                    >
                      <p v-if="node.quote" class="mb-1 border-l-2 border-emerald-300/50 pl-2 text-[11px] text-emerald-100/70 line-clamp-2">
                        {{ node.quote }}
                      </p>
                      {{ node.text }}
                    </div>

                    <div
                      v-else-if="node.kind === 'in' || node.kind === 'status'"
                      class="mr-auto max-w-[92%] rounded-2xl rounded-bl-md bg-[#1f2c34] px-3 py-2 text-[13px] leading-snug text-white/90"
                    >
                      <div v-html="node.html" />
                      <div v-if="node.kind === 'in' && node.buttons?.length" class="mt-2 space-y-1.5 border-t border-white/10 pt-2">
                        <div
                          v-for="btn in node.buttons"
                          :key="btn"
                          class="flex items-center justify-center gap-1.5 rounded-md border border-white/10 py-1.5 text-[12px] font-medium text-emerald-400"
                        >
                          {{ btn }}
                        </div>
                      </div>
                      <div
                        v-else-if="node.kind === 'status' && node.payNow"
                        class="mt-2 flex items-center justify-center gap-1.5 border-t border-white/10 pt-2 text-[12px] font-medium text-emerald-400"
                      >
                        Pay now
                      </div>
                    </div>

                    <div
                      v-else-if="node.kind === 'products'"
                      class="product-rail -mx-1 flex gap-2 overflow-x-auto pb-1 pl-0.5"
                    >
                      <article
                        v-for="item in node.items"
                        :key="item.name"
                        class="w-[148px] shrink-0 overflow-hidden rounded-xl border border-white/10 bg-[#1f2c34]"
                      >
                        <img
                          :src="item.img"
                          :alt="item.name"
                          class="h-[88px] w-full object-cover"
                          loading="eager"
                        />
                        <div class="space-y-1 p-2">
                          <p class="text-[12px] font-semibold text-white">{{ item.name }} ({{ item.price }})</p>
                          <p class="line-clamp-2 text-[10px] leading-snug text-white/50">{{ item.desc }}</p>
                          <div class="pt-1 text-center text-[11px] font-medium text-emerald-400">
                            Add to cart
                          </div>
                        </div>
                      </article>
                    </div>
                  </div>
                </template>

                <div
                  v-if="demoStep < demoNodes.length"
                  class="typing self-start flex gap-1 rounded-2xl bg-[#1f2c34] px-3 py-2.5"
                >
                  <span /><span /><span />
                </div>
              </div>

              <div class="flex items-center gap-2 border-t border-white/[0.06] bg-[#0b1410] px-3 py-2">
                <div class="flex-1 rounded-full bg-[#1f2c34] px-3 py-2 text-[12px] text-white/35">
                  Message
                </div>
              </div>
            </div>
            <p class="mt-3 text-center text-[11px] text-white/35">
              Live demo flow · Bakery Bengaluru on WhatsApp
            </p>
          </div>
        </div>
      </section>

      <!-- Problems -->
      <section id="problems" class="mx-auto max-w-6xl px-4 py-20 sm:px-6">
        <div
          ref="painsEl"
          class="transition-all duration-700"
          :class="painsShown ? 'translate-y-0 opacity-100' : 'translate-y-8 opacity-0'"
        >
          <p class="text-sm font-medium uppercase tracking-[0.18em] text-rose-400/90">The reality</p>
          <h2 class="mt-3 max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
            Are you facing these problems?
          </h2>
          <div class="mt-12 grid gap-5 md:grid-cols-3">
            <article
              v-for="(pain, i) in pains"
              :key="pain.title"
              class="reveal-card rounded-2xl border border-rose-500/15 bg-rose-500/[0.04] p-6 light:border-rose-200 light:bg-rose-50/60"
              :style="{ transitionDelay: painsShown ? `${i * 100}ms` : '0ms' }"
              :class="painsShown ? 'translate-y-0 opacity-100' : 'translate-y-6 opacity-0'"
            >
              <div class="mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-rose-500/15 text-rose-300 light:text-rose-600">
                <component :is="pain.icon" class="h-5 w-5" />
              </div>
              <h3 class="text-lg font-medium">{{ pain.title }}</h3>
              <p class="mt-2 text-sm leading-relaxed text-white/55 light:text-slate-600">{{ pain.body }}</p>
            </article>
          </div>
        </div>
      </section>

      <!-- Solution -->
      <section id="solution" class="border-y border-white/[0.06] bg-white/[0.015] py-20 light:border-slate-200 light:bg-slate-100/60">
        <div
          ref="capsEl"
          class="mx-auto max-w-6xl px-4 sm:px-6 transition-all duration-700"
          :class="capsShown ? 'translate-y-0 opacity-100' : 'translate-y-8 opacity-0'"
        >
          <p class="text-sm font-medium uppercase tracking-[0.18em] text-emerald-400/90">The fix</p>
          <h2 class="mt-3 max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
            Use TiQR Agent — your WhatsApp AI agent
          </h2>
          <p class="mt-4 max-w-2xl text-white/55 light:text-slate-600">
            Personalized recommendations, live order capture, and always-on replies — so customers never wait for a free hand on the floor.
          </p>
          <div class="mt-12 grid gap-5 md:grid-cols-3">
            <article
              v-for="(cap, i) in capabilities"
              :key="cap.title"
              class="reveal-card rounded-2xl border border-white/[0.08] bg-[#0c0d10] p-6 light:border-slate-200 light:bg-white"
              :style="{ transitionDelay: capsShown ? `${i * 100}ms` : '0ms' }"
              :class="capsShown ? 'translate-y-0 opacity-100' : 'translate-y-6 opacity-0'"
            >
              <div class="mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-emerald-500/15 text-emerald-400">
                <component :is="cap.icon" class="h-5 w-5" />
              </div>
              <h3 class="text-lg font-medium">{{ cap.title }}</h3>
              <p class="mt-2 text-sm leading-relaxed text-white/55 light:text-slate-600">{{ cap.body }}</p>
            </article>
          </div>
        </div>
      </section>

      <!-- Benefits -->
      <section id="benefits" class="mx-auto max-w-6xl px-4 py-20 sm:px-6">
        <div
          ref="benefitsEl"
          class="transition-all duration-700"
          :class="benefitsShown ? 'translate-y-0 opacity-100' : 'translate-y-8 opacity-0'"
        >
          <p class="text-sm font-medium uppercase tracking-[0.18em] text-emerald-400/90">The benefits</p>
          <h2 class="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">Why stores switch to TiQR Agent</h2>
          <div class="mt-12 grid gap-6 md:grid-cols-3">
            <article
              v-for="(benefit, i) in benefits"
              :key="benefit.title"
              class="reveal-card relative overflow-hidden rounded-2xl border border-white/[0.08] p-6 light:border-slate-200 light:bg-white"
              :style="{ transitionDelay: benefitsShown ? `${i * 120}ms` : '0ms' }"
              :class="benefitsShown ? 'translate-y-0 opacity-100' : 'translate-y-6 opacity-0'"
            >
              <div class="absolute -right-6 -top-6 h-24 w-24 rounded-full bg-emerald-500/10 blur-2xl" />
              <component :is="benefit.icon" class="relative h-8 w-8 text-emerald-400" />
              <h3 class="relative mt-5 text-xl font-semibold uppercase tracking-wide">{{ benefit.title }}</h3>
              <p class="relative mt-3 text-sm leading-relaxed text-white/55 light:text-slate-600">{{ benefit.body }}</p>
            </article>
          </div>
        </div>
      </section>

      <!-- Pricing -->
      <section id="pricing" class="border-y border-white/[0.06] bg-white/[0.015] py-20 light:border-slate-200 light:bg-slate-100/70">
        <div
          ref="pricingEl"
          class="mx-auto max-w-6xl px-4 sm:px-6 transition-all duration-700"
          :class="pricingShown ? 'translate-y-0 opacity-100' : 'translate-y-8 opacity-0'"
        >
          <div class="max-w-2xl">
            <h2 class="text-3xl font-semibold tracking-tight">AI agent pricing</h2>
            <p class="mt-3 text-white/55 light:text-slate-600">
              Sell the AI first. Pick a plan by monthly fee and included orders — extras bill cheaper as you grow.
            </p>
          </div>

          <div class="mt-10 grid gap-5 lg:grid-cols-3">
            <article
              v-for="plan in aiPlans"
              :key="plan.id"
              class="relative flex flex-col rounded-2xl border p-6 transition-transform duration-300 hover:-translate-y-1"
              :class="plan.highlight
                ? 'border-emerald-500/50 bg-emerald-500/10 shadow-lg shadow-emerald-900/20 light:bg-emerald-50 light:border-emerald-300'
                : 'border-white/[0.08] bg-[#0c0d10] light:border-slate-200 light:bg-white'"
            >
              <p
                v-if="plan.highlight"
                class="absolute -top-3 left-6 rounded-full bg-emerald-500 px-3 py-0.5 text-xs font-semibold text-white"
              >
                Most popular
              </p>
              <h3 class="text-xl font-semibold">{{ plan.name }}</h3>
              <p class="mt-1 text-sm text-white/50 light:text-slate-500">{{ plan.blurb }}</p>
              <p class="mt-5">
                <span class="text-3xl font-semibold">{{ formatInr(plan.monthlyFee) }}</span>
                <span class="text-sm text-white/45 light:text-slate-500"> / month</span>
              </p>
              <ul class="mt-5 space-y-2 text-sm text-white/70 light:text-slate-700">
                <li class="flex gap-2"><Check class="h-4 w-4 shrink-0 text-emerald-400" /> {{ plan.freeOrders }} free orders included</li>
                <li class="flex gap-2"><Check class="h-4 w-4 shrink-0 text-emerald-400" /> {{ formatInr(plan.perOrder) }} per extra order</li>
                <li class="flex gap-2"><Check class="h-4 w-4 shrink-0 text-emerald-400" /> Payment gateway as actuals</li>
              </ul>
              <Button
                class="mt-6 w-full bg-emerald-600 text-white hover:bg-emerald-500"
                @click="talkAboutPlan(plan.name)"
              >
                Talk about {{ plan.name }}
              </Button>
            </article>
          </div>

          <div class="mt-12 rounded-2xl border border-white/[0.08] bg-[#0c0d10] p-6 light:border-slate-200 light:bg-white sm:p-8">
            <div class="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
              <div class="max-w-md">
                <h3 class="text-xl font-semibold">Pricing calculator</h3>
                <p class="mt-2 text-sm text-white/50 light:text-slate-600">
                  Estimate monthly cost for your order volume. Formula: plan fee + max(0, orders − free) × per-order rate.
                </p>
              </div>
              <div class="w-full max-w-sm space-y-3">
                <Label for="orders" class="text-white/70 light:text-slate-700">Monthly orders</Label>
                <Input
                  id="orders"
                  v-model.number="orderCount"
                  type="number"
                  min="0"
                  step="50"
                  class="border-white/10 bg-white/[0.04] text-white light:border-slate-300 light:bg-white light:text-slate-900"
                />
                <div class="flex flex-wrap gap-2">
                  <button
                    v-for="preset in orderPresets"
                    :key="preset"
                    type="button"
                    class="rounded-full border border-white/10 px-3 py-1 text-xs text-white/70 hover:border-emerald-500/40 hover:text-white light:border-slate-300 light:text-slate-600 light:hover:border-emerald-400 light:hover:text-slate-900"
                    @click="orderCount = preset"
                  >
                    {{ preset.toLocaleString('en-IN') }}
                  </button>
                </div>
              </div>
            </div>

            <div class="mt-8 grid gap-3 sm:grid-cols-3">
              <div
                v-for="row in planTotals"
                :key="row.id"
                class="rounded-xl border p-4 transition-colors duration-300"
                :class="row.id === cheapestPlanId
                  ? 'border-emerald-500/60 bg-emerald-500/10 light:border-emerald-400 light:bg-emerald-50'
                  : 'border-white/[0.08] bg-white/[0.02] light:border-slate-200 light:bg-slate-50'"
              >
                <div class="flex items-center justify-between gap-2">
                  <p class="font-medium">{{ row.name }}</p>
                  <span
                    v-if="row.id === cheapestPlanId"
                    class="rounded-full bg-emerald-500/20 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-300 light:text-emerald-700"
                  >
                    Best at this volume
                  </span>
                </div>
                <p class="mt-3 text-2xl font-semibold tabular-nums">{{ formatInr(row.total) }}</p>
                <p class="mt-1 text-xs text-white/45 light:text-slate-500">
                  {{ row.extra > 0 ? `${row.extra} extra orders` : 'Within free allowance' }}
                </p>
              </div>
            </div>
          </div>

          <div class="mt-10 rounded-xl border border-dashed border-white/10 px-5 py-4 text-sm text-white/55 light:border-slate-300 light:text-slate-600">
            <p class="font-medium text-white/80 light:text-slate-800">
              Prefer WhatsApp tools without AI?
            </p>
            <p class="mt-1">
              <span class="text-white light:text-slate-900">Basic — {{ formatInr(999) }}/month</span>
              includes core WhatsApp Business features (inbox, templates, messaging). No AI agent.
              Payment gateway charges as actuals.
              <button
                type="button"
                class="ml-1 font-medium text-emerald-400 hover:underline"
                @click="talkAboutPlan('Basic')"
              >
                Ask about Basic
              </button>
            </p>
          </div>

          <p class="mt-6 text-xs text-white/35 light:text-slate-500">
            Charges are per store per month. If WhatsApp message pricing changes substantially, these fees may be adjusted — expected to be rare.
          </p>

          <!-- Pricing & demo FAQs -->
          <div id="faq" class="mt-16 scroll-mt-24">
            <h3 class="text-2xl font-semibold tracking-tight">Pricing &amp; demo FAQs</h3>
            <p class="mt-2 max-w-2xl text-sm text-white/55 light:text-slate-600">
              Common questions about plans, the WhatsApp demo, and custom work.
            </p>
            <dl class="mt-8 space-y-4">
              <div
                v-for="faq in faqs"
                :key="faq.q"
                class="rounded-xl border border-white/[0.08] bg-[#0c0d10] px-5 py-4 light:border-slate-200 light:bg-white"
              >
                <dt class="font-medium text-white light:text-slate-900">{{ faq.q }}</dt>
                <dd class="mt-2 text-sm leading-relaxed text-white/55 light:text-slate-600">
                  {{ faq.a }}
                  <button
                    v-if="faq.contact"
                    type="button"
                    class="ml-1 font-medium text-emerald-400 hover:underline"
                    @click="scrollToLead"
                  >
                    Contact us
                  </button>
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </section>

      <!-- Lead -->
      <section id="lead" class="mx-auto max-w-6xl px-4 py-20 sm:px-6">
        <div class="grid gap-10 lg:grid-cols-2 lg:items-start">
          <div>
            <h2 class="text-3xl font-semibold tracking-tight">Ready to stop missing orders?</h2>
            <p class="mt-3 text-white/55 light:text-slate-600">
              Tell us about your store. We’ll open WhatsApp so you can finish the conversation with our team.
            </p>
            <ul class="mt-8 space-y-3 text-sm text-white/65 light:text-slate-600">
              <li class="flex gap-2"><Clock class="h-4 w-4 shrink-0 text-emerald-400" /> Usually reply within a few hours</li>
              <li class="flex gap-2"><MessageSquare class="h-4 w-4 shrink-0 text-emerald-400" /> +91 77959 37091</li>
              <li class="flex gap-2"><Check class="h-4 w-4 shrink-0 text-emerald-400" /> {{ SUPPORT_EMAIL }}</li>
            </ul>
            <p class="mt-8 text-xs leading-relaxed text-white/35 light:text-slate-500">
              TiQR Labs Private Limited · Kannur, Kerala<br />
              GST 32AAKCT2452N1Z3 · hello@tiqr.events
            </p>
          </div>
          <form
            class="space-y-4 rounded-2xl border border-white/[0.08] bg-white/[0.02] p-6 light:border-slate-200 light:bg-white"
            @submit.prevent="submitLead"
          >
            <div class="space-y-2">
              <Label for="lead-name" class="text-white/70 light:text-slate-700">Your name</Label>
              <Input
                id="lead-name"
                v-model="lead.name"
                required
                placeholder="Priya Sharma"
                class="border-white/10 bg-white/[0.04] text-white light:border-slate-300 light:bg-white light:text-slate-900"
              />
            </div>
            <div class="space-y-2">
              <Label for="lead-business" class="text-white/70 light:text-slate-700">Business / store</Label>
              <Input
                id="lead-business"
                v-model="lead.business"
                required
                placeholder="Your store name"
                class="border-white/10 bg-white/[0.04] text-white light:border-slate-300 light:bg-white light:text-slate-900"
              />
            </div>
            <div class="space-y-2">
              <Label for="lead-phone" class="text-white/70 light:text-slate-700">Phone (optional)</Label>
              <Input
                id="lead-phone"
                v-model="lead.phone"
                type="tel"
                placeholder="+91 …"
                class="border-white/10 bg-white/[0.04] text-white light:border-slate-300 light:bg-white light:text-slate-900"
              />
            </div>
            <div class="space-y-2">
              <Label for="lead-plan" class="text-white/70 light:text-slate-700">Plan interest</Label>
              <select
                id="lead-plan"
                v-model="lead.plan"
                class="flex h-10 w-full rounded-md border border-white/10 bg-white/[0.04] px-3 text-sm text-white light:border-slate-300 light:bg-white light:text-slate-900"
              >
                <option>AI Base</option>
                <option>AI Advanced</option>
                <option>AI Pro</option>
                <option>Basic</option>
                <option>Not sure yet</option>
              </select>
            </div>
            <Button type="submit" class="w-full bg-emerald-600 text-white hover:bg-emerald-500" size="lg">
              Continue on WhatsApp
            </Button>
            <p class="text-center text-xs text-white/40 light:text-slate-500">
              Opens WhatsApp chat with +91 77959 37091
            </p>
          </form>
        </div>
      </section>
    </main>

    <footer class="border-t border-white/[0.06] py-10 light:border-slate-200">
      <div class="mx-auto flex max-w-6xl flex-col gap-4 px-4 text-sm text-white/45 sm:flex-row sm:items-center sm:justify-between sm:px-6 light:text-slate-500">
        <p>© {{ new Date().getFullYear() }} TiQR Agent · TiQR Labs Pvt Ltd</p>
        <div class="flex flex-wrap gap-4">
          <RouterLink to="/login" class="hover:text-white light:hover:text-slate-900">Sign in</RouterLink>
          <a :href="`mailto:${SUPPORT_EMAIL}`" class="hover:text-white light:hover:text-slate-900">{{ SUPPORT_EMAIL }}</a>
          <a
            :href="`https://wa.me/${SUPPORT_PHONE}`"
            target="_blank"
            rel="noopener noreferrer"
            class="hover:text-white light:hover:text-slate-900"
          >
            WhatsApp
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.reveal-card {
  transition: opacity 0.65s ease, transform 0.65s ease;
}

.chat-bubble {
  animation: bubble-in 0.35s ease-out;
}

@keyframes bubble-in {
  from {
    opacity: 0;
    transform: translateY(8px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.online-dot {
  animation: pulse-dot 1.6s ease-in-out infinite;
}

@keyframes pulse-dot {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.45;
    transform: scale(0.85);
  }
}

.typing span {
  display: inline-block;
  width: 5px;
  height: 5px;
  border-radius: 9999px;
  background: rgba(255, 255, 255, 0.45);
  animation: typing 1.1s ease-in-out infinite;
}

.typing span:nth-child(2) {
  animation-delay: 0.15s;
}

.typing span:nth-child(3) {
  animation-delay: 0.3s;
}

@keyframes typing {
  0%,
  60%,
  100% {
    transform: translateY(0);
    opacity: 0.35;
  }
  30% {
    transform: translateY(-3px);
    opacity: 1;
  }
}

.cta-pulse {
  animation: cta-glow 2.8s ease-in-out infinite;
}

@keyframes cta-glow {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.35);
  }
  50% {
    box-shadow: 0 0 0 10px rgba(16, 185, 129, 0);
  }
}

.demo-chat {
  background-color: #0b1410;
  background-image: radial-gradient(rgba(255, 255, 255, 0.03) 1px, transparent 1px);
  background-size: 18px 18px;
  scrollbar-width: none;
}

.demo-chat::-webkit-scrollbar {
  display: none;
}

.product-rail {
  scrollbar-width: none;
  animation: rail-nudge 2.2s ease-in-out;
}

.product-rail::-webkit-scrollbar {
  display: none;
}

@keyframes rail-nudge {
  0%,
  100% {
    transform: translateX(0);
  }
  40% {
    transform: translateX(-28px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .reveal-card,
  .chat-bubble,
  .cta-pulse,
  .online-dot,
  .typing span,
  .product-rail {
    animation: none !important;
    transition: none !important;
  }
}
</style>
