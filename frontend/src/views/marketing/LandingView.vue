<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'

const WHATSAPP_NUMBER = '919846728292'
const WHATSAPP_DISPLAY = '+91 98467 28292'

function whatsappUrl(message: string) {
  return `https://wa.me/${WHATSAPP_NUMBER}?text=${encodeURIComponent(message)}`
}

const tryOutUrl = whatsappUrl(
  "Hi! I'd like to try out tiqr.store — the WhatsApp AI shop assistant for my store."
)
const sellTryUrl = whatsappUrl(
  "Hi! I'm interested in the Sell plan on tiqr.store. Can we set up a demo?"
)
const aiTryUrl = whatsappUrl(
  "Hi! I'm interested in the AI Seller plan on tiqr.store. Can we set up a demo?"
)
const contactUrl = whatsappUrl(
  "Hi! I'd like to learn more about tiqr.store for my business."
)

const logoSrc =
  ((window as any).__BASE_PATH__ ?? import.meta.env.BASE_URL ?? '/').replace(/\/?$/, '/') +
  'logo_with_name.png'

const menuOpen = ref(false)
const heroReady = ref(false)

const features = [
  {
    title: 'Shared team inbox',
    body: 'Every WhatsApp conversation in one place — so the right person replies, and nothing lives only on a personal phone.',
  },
  {
    title: 'Live catalog on chat',
    body: 'Product cards pull from your TiQR Store catalog: name, image, price, options, and stock — not screenshots.',
  },
  {
    title: 'AI shop assistant',
    body: 'Searches your catalog, shows products, confirms the order, then creates it on TiQR — with a human handoff when needed.',
  },
  {
    title: 'Automation that never sleeps',
    body: 'Keyword replies and conversation flows handle FAQs, greetings, and after-hours so your team focuses on buyers.',
  },
  {
    title: 'Campaigns & templates',
    body: 'Meta-approved templates for launches, restocks, and offers — with retries and clear delivery tracking.',
  },
  {
    title: 'Orders stay on TiQR',
    body: 'Cart, checkout, and payments remain on your store. Buyers get status updates on WhatsApp when the order moves.',
  },
  {
    title: 'Team control & insights',
    body: 'Roles, permissions, and analytics so you see what your inbox and campaigns are doing.',
  },
]

let observer: IntersectionObserver | null = null

onMounted(() => {
  requestAnimationFrame(() => {
    heroReady.value = true
  })

  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add('is-visible')
          observer?.unobserve(entry.target)
        }
      }
    },
    { threshold: 0.12, rootMargin: '0px 0px -40px 0px' }
  )

  document.querySelectorAll('.landing .reveal').forEach((el) => observer?.observe(el))
})

onUnmounted(() => {
  observer?.disconnect()
})

function closeMenu() {
  menuOpen.value = false
}
</script>

<template>
  <div class="landing">
    <header class="nav">
      <a href="#top" class="nav-brand" @click="closeMenu">
        <img :src="logoSrc" alt="tiqr.store" class="nav-logo" width="160" height="40" />
      </a>

      <button
        type="button"
        class="nav-toggle"
        :aria-expanded="menuOpen"
        aria-label="Toggle menu"
        @click="menuOpen = !menuOpen"
      >
        <span />
        <span />
      </button>

      <nav class="nav-links" :class="{ open: menuOpen }" aria-label="Primary">
        <a href="#features" @click="closeMenu">Features</a>
        <a href="#pricing" @click="closeMenu">Pricing</a>
        <a href="#contact" @click="closeMenu">Contact</a>
        <RouterLink class="btn btn-ghost" to="/login" @click="closeMenu">Login</RouterLink>
        <a class="btn btn-primary" :href="tryOutUrl" target="_blank" rel="noopener noreferrer" @click="closeMenu">
          Try Out
        </a>
      </nav>
    </header>

    <main id="top">
      <section class="hero" :class="{ ready: heroReady }">
        <div class="hero-atmosphere" aria-hidden="true" />
        <div class="hero-inner">
          <img :src="logoSrc" alt="tiqr.store" class="hero-logo" width="280" height="70" />
          <h1 class="hero-title">Sell on WhatsApp with the store you already run on TiQR.</h1>
          <p class="hero-lede">
            Official WhatsApp for your store — team inbox, campaigns, automation, and an AI shop
            assistant that knows your live catalog.
          </p>
          <div class="hero-ctas">
            <a class="btn btn-primary btn-lg" :href="tryOutUrl" target="_blank" rel="noopener noreferrer">
              Try Out
            </a>
            <RouterLink class="btn btn-secondary btn-lg" to="/login">Login</RouterLink>
          </div>
        </div>
      </section>

      <section id="features" class="section features">
        <div class="section-inner">
          <p class="kicker reveal">Features</p>
          <h2 class="section-title reveal">WhatsApp that closes TiQR Store orders</h2>
          <p class="section-lede reveal">
            One sales layer on top of the catalog, cart, and checkout you already trust.
          </p>

          <div class="feature-list">
            <article
              v-for="(feature, index) in features"
              :key="feature.title"
              class="feature-row reveal"
            >
              <span class="feature-index">{{ String(index + 1).padStart(2, '0') }}</span>
              <div>
                <h3>{{ feature.title }}</h3>
                <p>{{ feature.body }}</p>
              </div>
            </article>
          </div>
        </div>
      </section>

      <section id="pricing" class="section pricing">
        <div class="section-inner">
          <p class="kicker reveal">Pricing</p>
          <h2 class="section-title reveal">Simple plans for selling stores</h2>
          <p class="section-lede reveal">
            Unlimited team seats on every plan. Meta template fees are billed at Meta’s rates.
          </p>

          <div class="pricing-grid">
            <article class="price-card reveal">
              <h3>Sell</h3>
              <p class="price">
                <span class="amount">₹2,999</span>
                <span class="period">/ month</span>
              </p>
              <ul>
                <li>Shared inbox &amp; live chat</li>
                <li>Campaigns &amp; Meta templates</li>
                <li>Keyword bots &amp; conversation flows</li>
                <li>TiQR catalog product cards</li>
                <li>Unlimited seats</li>
                <li>1 WhatsApp number</li>
              </ul>
              <a class="btn btn-secondary btn-block" :href="sellTryUrl" target="_blank" rel="noopener noreferrer">
                Try Out
              </a>
            </article>

            <article class="price-card featured reveal">
              <p class="badge">Most popular</p>
              <h3>AI Seller</h3>
              <p class="price">
                <span class="amount">₹5,999</span>
                <span class="period">/ month</span>
              </p>
              <ul>
                <li>Everything in Sell</li>
                <li>AI shop assistant on WhatsApp</li>
                <li>Live catalog search &amp; recommendations</li>
                <li>Order assist on TiQR</li>
                <li>3,000 AI replies / month</li>
                <li>Unlimited seats</li>
              </ul>
              <a class="btn btn-primary btn-block" :href="aiTryUrl" target="_blank" rel="noopener noreferrer">
                Try Out
              </a>
            </article>
          </div>

          <p class="pricing-note reveal">
            Prices exclude GST. Meta conversation / template fees are pass-through at Meta’s published
            rates. Annual billing includes 2 months free. Extra WhatsApp numbers available on request.
          </p>
        </div>
      </section>

      <section id="contact" class="section contact">
        <div class="section-inner contact-inner reveal">
          <p class="kicker">Contact</p>
          <h2 class="section-title">Talk to us on WhatsApp</h2>
          <p class="section-lede">
            New to tiqr.store? Tap Try Out and we’ll help you connect your number and walk through a
            live demo on your catalog.
          </p>
          <a class="btn btn-primary btn-lg" :href="contactUrl" target="_blank" rel="noopener noreferrer">
            WhatsApp {{ WHATSAPP_DISPLAY }}
          </a>
          <p class="contact-number">
            Or message
            <a :href="contactUrl" target="_blank" rel="noopener noreferrer">{{ WHATSAPP_DISPLAY }}</a>
            anytime.
          </p>
        </div>
      </section>
    </main>

    <footer class="footer">
      <div class="footer-inner">
        <img :src="logoSrc" alt="tiqr.store" class="footer-logo" width="140" height="36" />
        <div class="footer-links">
          <RouterLink to="/login">Login</RouterLink>
          <a :href="tryOutUrl" target="_blank" rel="noopener noreferrer">Try Out</a>
          <a href="#contact">Contact</a>
        </div>
        <p class="footer-copy">© {{ new Date().getFullYear() }} tiqr.store</p>
      </div>
    </footer>
  </div>
</template>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,400;0,9..40,500;0,9..40,600;0,9..40,700;1,9..40,400&family=Fraunces:opsz,wght@9..144,500;9..144,600;9..144,700&display=swap');

.landing {
  --green: #213a2f;
  --green-soft: #2d4a3c;
  --gold: #b59341;
  --gold-soft: rgba(181, 147, 65, 0.16);
  --bg: #f7f5f0;
  --bg-deep: #ebe7df;
  --ink: #213a2f;
  --muted: #5c6f66;
  --line: rgba(33, 58, 47, 0.12);
  --white: #ffffff;

  min-height: 100vh;
  background: var(--bg);
  color: var(--ink);
  font-family: 'DM Sans', system-ui, sans-serif;
  font-optical-sizing: auto;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}

.landing * {
  box-sizing: border-box;
}

.nav {
  position: sticky;
  top: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.85rem clamp(1.25rem, 4vw, 3rem);
  background: rgba(247, 245, 240, 0.92);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--line);
}

.nav-brand {
  display: flex;
  align-items: center;
  text-decoration: none;
}

.nav-logo {
  height: 36px;
  width: auto;
  display: block;
}

.nav-toggle {
  display: none;
  width: 2.5rem;
  height: 2.5rem;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: transparent;
  padding: 0.65rem;
  flex-direction: column;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
}

.nav-toggle span {
  display: block;
  height: 2px;
  background: var(--green);
  border-radius: 2px;
}

.nav-links {
  display: flex;
  align-items: center;
  gap: 0.35rem 1.1rem;
}

.nav-links a:not(.btn) {
  color: var(--muted);
  text-decoration: none;
  font-size: 0.95rem;
  font-weight: 500;
}

.nav-links a:not(.btn):hover {
  color: var(--green);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.4rem;
  border-radius: 999px;
  padding: 0.55rem 1.15rem;
  font-size: 0.92rem;
  font-weight: 600;
  text-decoration: none;
  border: 1px solid transparent;
  transition: transform 0.18s ease, background 0.18s ease, border-color 0.18s ease, color 0.18s ease;
  cursor: pointer;
}

.btn:hover {
  transform: translateY(-1px) scale(1.02);
}

.btn:active {
  transform: scale(0.98);
}

.btn-primary {
  background: var(--green);
  color: var(--white);
}

.btn-primary:hover {
  background: var(--green-soft);
}

.btn-secondary {
  background: transparent;
  color: var(--green);
  border-color: var(--line);
}

.btn-secondary:hover {
  border-color: var(--green);
  background: rgba(33, 58, 47, 0.04);
}

.btn-ghost {
  background: transparent;
  color: var(--green);
}

.btn-lg {
  padding: 0.85rem 1.5rem;
  font-size: 1rem;
}

.btn-block {
  width: 100%;
  margin-top: auto;
}

.hero {
  position: relative;
  min-height: min(92vh, 880px);
  display: flex;
  align-items: center;
  padding: clamp(3rem, 8vw, 6rem) clamp(1.25rem, 4vw, 3rem) clamp(4rem, 10vw, 7rem);
  overflow: hidden;
}

.hero-atmosphere {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse 70% 55% at 85% 10%, rgba(181, 147, 65, 0.18), transparent 55%),
    radial-gradient(ellipse 55% 45% at 5% 90%, rgba(33, 58, 47, 0.1), transparent 50%),
    linear-gradient(180deg, var(--bg) 0%, var(--bg-deep) 100%);
  pointer-events: none;
}

.hero-atmosphere::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(rgba(33, 58, 47, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(33, 58, 47, 0.05) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(ellipse 65% 55% at 40% 40%, black, transparent);
  opacity: 0.7;
}

.hero-inner {
  position: relative;
  z-index: 1;
  max-width: 720px;
  opacity: 0;
  transform: translateY(18px);
  transition: opacity 0.7s ease, transform 0.7s ease;
}

.hero.ready .hero-inner {
  opacity: 1;
  transform: translateY(0);
}

.hero-logo {
  height: clamp(48px, 7vw, 70px);
  width: auto;
  margin-bottom: 1.75rem;
  display: block;
}

.hero-title {
  font-family: 'Fraunces', Georgia, serif;
  font-weight: 600;
  font-size: clamp(2.15rem, 5.2vw, 3.6rem);
  line-height: 1.08;
  letter-spacing: -0.02em;
  max-width: 14ch;
  margin: 0 0 1.15rem;
  color: var(--green);
}

.hero-lede {
  font-size: clamp(1.05rem, 1.7vw, 1.25rem);
  color: var(--muted);
  max-width: 38ch;
  margin: 0 0 1.75rem;
}

.hero-ctas {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
}

.section {
  padding: clamp(4rem, 9vw, 6.5rem) clamp(1.25rem, 4vw, 3rem);
}

.section-inner {
  max-width: 1080px;
  margin: 0 auto;
}

.kicker {
  display: inline-flex;
  align-items: center;
  gap: 0.55rem;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--gold);
  margin: 0 0 0.85rem;
}

.kicker::before {
  content: '';
  width: 1.35rem;
  height: 2px;
  background: var(--gold);
}

.section-title {
  font-family: 'Fraunces', Georgia, serif;
  font-weight: 600;
  font-size: clamp(1.75rem, 3.4vw, 2.6rem);
  line-height: 1.15;
  letter-spacing: -0.02em;
  margin: 0 0 0.75rem;
  max-width: 18ch;
  color: var(--green);
}

.section-lede {
  color: var(--muted);
  font-size: 1.05rem;
  max-width: 42ch;
  margin: 0 0 2.5rem;
}

.features {
  background: var(--white);
  border-top: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
}

.feature-list {
  display: grid;
  gap: 0;
}

.feature-row {
  display: grid;
  grid-template-columns: 3.5rem 1fr;
  gap: 1rem;
  padding: 1.35rem 0;
  border-top: 1px solid var(--line);
}

.feature-row:last-child {
  border-bottom: 1px solid var(--line);
}

.feature-index {
  font-family: 'Fraunces', Georgia, serif;
  font-size: 1.25rem;
  color: var(--gold);
  padding-top: 0.15rem;
}

.feature-row h3 {
  margin: 0 0 0.35rem;
  font-size: 1.15rem;
  font-weight: 650;
  color: var(--green);
}

.feature-row p {
  margin: 0;
  color: var(--muted);
  max-width: 52ch;
}

.pricing {
  background:
    radial-gradient(ellipse 50% 40% at 100% 0%, rgba(181, 147, 65, 0.12), transparent 55%),
    var(--bg);
}

.pricing-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1.25rem;
  align-items: stretch;
}

.price-card {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
  background: var(--white);
  border: 1px solid var(--line);
  border-radius: 20px;
  padding: 1.6rem 1.5rem 1.5rem;
  min-height: 100%;
}

.price-card.featured {
  border-color: rgba(33, 58, 47, 0.35);
  box-shadow: 0 0 0 1px rgba(33, 58, 47, 0.08);
  background:
    linear-gradient(180deg, rgba(33, 58, 47, 0.03), transparent 40%),
    var(--white);
}

.badge {
  align-self: flex-start;
  margin: 0;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--gold);
  background: var(--gold-soft);
  padding: 0.3rem 0.65rem;
  border-radius: 999px;
}

.price-card h3 {
  margin: 0;
  font-family: 'Fraunces', Georgia, serif;
  font-size: 1.65rem;
  font-weight: 600;
}

.price {
  margin: 0;
  display: flex;
  align-items: baseline;
  gap: 0.35rem;
}

.amount {
  font-family: 'Fraunces', Georgia, serif;
  font-size: 2.4rem;
  font-weight: 600;
  letter-spacing: -0.02em;
  color: var(--green);
}

.period {
  color: var(--muted);
  font-size: 0.95rem;
}

.price-card ul {
  list-style: none;
  margin: 0.25rem 0 0.5rem;
  padding: 0;
  display: grid;
  gap: 0.55rem;
  flex: 1;
}

.price-card li {
  position: relative;
  padding-left: 1.1rem;
  color: var(--muted);
  font-size: 0.95rem;
}

.price-card li::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0.55rem;
  width: 0.4rem;
  height: 0.4rem;
  border-radius: 50%;
  background: var(--gold);
}

.pricing-note {
  margin: 1.5rem 0 0;
  font-size: 0.88rem;
  color: var(--muted);
  max-width: 62ch;
}

.contact {
  background: var(--green);
  color: var(--white);
}

.contact .kicker {
  color: var(--gold);
}

.contact .kicker::before {
  background: var(--gold);
}

.contact .section-title {
  color: var(--white);
  max-width: 16ch;
}

.contact .section-lede {
  color: rgba(247, 245, 240, 0.78);
}

.contact-inner {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.35rem;
}

.contact .btn-primary {
  background: var(--gold);
  color: var(--green);
  margin-top: 0.75rem;
}

.contact .btn-primary:hover {
  background: #c9a64f;
}

.contact-number {
  margin: 1rem 0 0;
  color: rgba(247, 245, 240, 0.7);
  font-size: 0.95rem;
}

.contact-number a {
  color: var(--white);
  font-weight: 600;
}

.footer {
  background: #182820;
  color: rgba(247, 245, 240, 0.7);
  padding: 2rem clamp(1.25rem, 4vw, 3rem);
}

.footer-inner {
  max-width: 1080px;
  margin: 0 auto;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.footer-logo {
  height: 32px;
  width: auto;
  filter: brightness(0) invert(1);
  opacity: 0.92;
}

.footer-links {
  display: flex;
  gap: 1.25rem;
}

.footer-links a {
  color: rgba(247, 245, 240, 0.78);
  text-decoration: none;
  font-size: 0.92rem;
  font-weight: 500;
}

.footer-links a:hover {
  color: var(--white);
}

.footer-copy {
  margin: 0;
  font-size: 0.85rem;
  width: 100%;
}

.reveal {
  opacity: 0;
  transform: translateY(16px);
  transition: opacity 0.55s ease, transform 0.55s ease;
}

.reveal.is-visible {
  opacity: 1;
  transform: translateY(0);
}

@media (max-width: 860px) {
  .pricing-grid {
    grid-template-columns: 1fr;
  }

  .nav-toggle {
    display: flex;
  }

  .nav-links {
    display: none;
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    flex-direction: column;
    align-items: stretch;
    gap: 0.35rem;
    padding: 1rem 1.25rem 1.25rem;
    background: var(--bg);
    border-bottom: 1px solid var(--line);
  }

  .nav-links.open {
    display: flex;
  }

  .nav-links .btn {
    width: 100%;
  }

  .feature-row {
    grid-template-columns: 2.5rem 1fr;
  }

  .hero-title {
    max-width: none;
  }
}

@media (max-width: 520px) {
  .hero-ctas {
    flex-direction: column;
    align-items: stretch;
  }

  .hero-ctas .btn {
    width: 100%;
  }

  .footer-inner {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
