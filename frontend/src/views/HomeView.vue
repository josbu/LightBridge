<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Default Home Page -->
  <div v-else class="min-h-screen overflow-hidden bg-gray-50/50 text-gray-950 dark:bg-dark-950/90 dark:text-white bg-mesh-gradient relative flex flex-col">
    <!-- Grid overlay pattern -->
    <div class="absolute inset-0 bg-grid opacity-75 pointer-events-none z-0"></div>
    
    <!-- Canvas particle background -->
    <canvas ref="particleCanvas" class="absolute inset-0 pointer-events-none z-10 opacity-70 dark:opacity-80"></canvas>

    <!-- Navigation Header -->
    <header class="relative z-30 border-b border-gray-200/40 bg-white/40 px-6 py-4 backdrop-blur-md dark:border-dark-800/40 dark:bg-dark-950/40">
      <nav class="mx-auto flex max-w-7xl items-center justify-between">
        <router-link to="/home" class="flex items-center gap-3">
          <span class="flex h-10 w-10 overflow-hidden rounded-xl border border-gray-200/20 bg-white shadow-sm dark:bg-dark-900">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="hidden text-sm font-semibold tracking-wide text-gray-900 dark:text-white sm:inline">{{ siteName }}</span>
        </router-link>

        <div class="flex items-center gap-2">
          <router-link
            to="/docs"
            class="flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-all hover:scale-105 hover:bg-teal-500/10 hover:text-teal-600 dark:text-gray-400 dark:hover:text-teal-400"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </router-link>
          <LocaleSwitcher />
          <button
            @click="toggleTheme"
            class="flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-all hover:scale-105 hover:bg-teal-500/10 hover:text-teal-600 dark:text-gray-400 dark:hover:text-teal-400"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex h-9 items-center rounded-lg bg-primary-500 px-4 text-sm font-semibold text-white shadow-sm shadow-primary-500/20 transition-all hover:bg-primary-600 active:scale-[0.98] hover:shadow-glow"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="flex-1 relative z-20">
      <!-- Hero Section -->
      <section class="relative border-b border-gray-200/20 dark:border-dark-800/20">
        <div class="relative mx-auto grid min-h-[calc(100vh-73px)] max-w-7xl items-center gap-12 px-6 py-16 lg:grid-cols-[1fr_520px] lg:py-20">
          <div class="max-w-3xl fade-up">
            <div class="mb-6 inline-flex items-center gap-2 rounded-full border border-primary-500/20 bg-primary-500/5 px-4.5 py-1.5 text-xs font-mono font-bold tracking-wider text-primary-600 dark:border-primary-500/20 dark:bg-primary-500/10 dark:text-primary-400 uppercase">
              <span class="h-1.5 w-1.5 rounded-full bg-primary-500 animate-pulse"></span>
              {{ t('home.heroSubtitle') }}
            </div>
            <h1 class="text-5xl font-black leading-none tracking-tight text-transparent bg-clip-text bg-gradient-to-r from-gray-950 via-primary-600 to-gray-950 dark:from-white dark:via-primary-400 dark:to-white sm:text-6xl lg:text-7xl">
              {{ siteName }}
            </h1>
            <p class="mt-6 max-w-2xl text-lg leading-8 text-gray-600 dark:text-dark-300 md:text-xl font-mono">
              // {{ siteSubtitle }}
            </p>
            <div class="mt-9 flex flex-col gap-3 sm:flex-row">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="inline-flex h-12 items-center justify-center rounded-xl bg-primary-500 px-6 text-sm font-bold text-white shadow-glow hover:shadow-glow-lg transition-all active:scale-[0.98] hover:bg-primary-600"
              >
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="sm" class="ml-2" :stroke-width="2" />
              </router-link>
              <router-link
                to="/docs"
                class="inline-flex h-12 items-center justify-center rounded-xl border border-gray-200 bg-white/50 px-6 text-sm font-bold text-gray-700 hover:text-primary-600 hover:border-primary-500/40 backdrop-blur-sm transition-all dark:border-dark-800 dark:bg-dark-900/40 dark:text-dark-200 dark:hover:text-primary-400 dark:hover:border-primary-500/30"
              >
                <Icon name="book" size="sm" class="mr-2" />
                {{ t('home.viewDocs') }}
              </router-link>
            </div>
          </div>

          <!-- Interactive Mock Terminal Panel -->
          <div class="relative fade-up fade-up-delay-1 max-w-md mx-auto w-full">
            <!-- Neon background glow -->
            <div class="absolute -inset-1 rounded-2xl bg-gradient-to-r from-primary-500/20 to-indigo-500/20 blur opacity-75 group-hover:opacity-100 transition duration-1000 group-hover:duration-200"></div>
            
            <div class="relative overflow-hidden rounded-2xl border border-gray-200/80 bg-gray-950 shadow-2xl dark:border-dark-800/80">
              <!-- Window Bar -->
              <div class="flex items-center justify-between border-b border-white/10 bg-gray-900/60 px-4 py-3">
                <div class="flex gap-1.5">
                  <span class="h-2.5 w-2.5 rounded-full bg-rose-500/80"></span>
                  <span class="h-2.5 w-2.5 rounded-full bg-amber-500/80"></span>
                  <span class="h-2.5 w-2.5 rounded-full bg-emerald-500/80"></span>
                </div>
                <span class="font-mono text-[10px] font-bold tracking-widest text-gray-500 uppercase">Unified Routing Core</span>
                <div class="w-10"></div>
              </div>
              
              <!-- Console Logs -->
              <div class="space-y-2 p-5 font-mono text-xs leading-6 text-gray-200 md:p-6 select-none h-60 overflow-y-auto">
                <div v-for="(line, idx) in terminalLines" :key="idx" class="flex flex-wrap items-start">
                  <template v-if="line.startsWith('$')">
                    <span class="text-primary-400 mr-2 font-bold select-none">$</span>
                    <span>{{ line.substring(2) }}</span>
                    <span v-if="idx === terminalLines.length - 1 && isTyping" class="cursor ml-1"></span>
                  </template>
                  <template v-else-if="line.startsWith('routing:')">
                    <span class="text-teal-400 font-bold">{{ line }}</span>
                  </template>
                  <template v-else-if="line.startsWith('HTTP/1.1 200')">
                    <span class="rounded bg-teal-500/20 px-1.5 py-0.5 text-teal-300 border border-teal-500/30 text-[10px] mr-2">200 OK</span>
                    <span class="text-gray-400">{{ line.substring(12) }}</span>
                  </template>
                  <template v-else>
                    <span class="text-gray-400 whitespace-pre">{{ line }}</span>
                  </template>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Feature Grid Section -->
      <section class="bg-gray-100/10 px-6 py-20 dark:bg-dark-950/20 border-b border-gray-200/10">
        <div class="mx-auto grid max-w-7xl gap-6 md:grid-cols-3">
          <div class="backdrop-blur-md bg-white/40 dark:bg-dark-900/40 border border-gray-200/50 dark:border-dark-800/50 rounded-2xl p-8 hover:border-primary-500/30 dark:hover:border-primary-500/20 shadow-glass transition-all duration-300 hover:-translate-y-1 hover:shadow-glow/5 relative overflow-hidden group">
            <div class="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-transparent via-primary-500/40 to-transparent opacity-0 group-hover:opacity-100 transition-opacity"></div>
            <div class="inline-flex p-3 rounded-xl bg-primary-500/10 text-primary-500 dark:bg-primary-500/20 dark:text-primary-400 mb-5">
              <Icon name="server" size="lg" />
            </div>
            <h2 class="text-lg font-bold text-gray-950 dark:text-white">{{ t('home.features.unifiedGateway') }}</h2>
            <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-300 font-mono">{{ t('home.features.unifiedGatewayDesc') }}</p>
          </div>
          
          <div class="backdrop-blur-md bg-white/40 dark:bg-dark-900/40 border border-gray-200/50 dark:border-dark-800/50 rounded-2xl p-8 hover:border-primary-500/30 dark:hover:border-primary-500/20 shadow-glass transition-all duration-300 hover:-translate-y-1 hover:shadow-glow/5 relative overflow-hidden group">
            <div class="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-transparent via-primary-500/40 to-transparent opacity-0 group-hover:opacity-100 transition-opacity"></div>
            <div class="inline-flex p-3 rounded-xl bg-primary-500/10 text-primary-500 dark:bg-primary-500/20 dark:text-primary-400 mb-5">
              <Icon name="shield" size="lg" />
            </div>
            <h2 class="text-lg font-bold text-gray-950 dark:text-white">{{ t('home.features.multiAccount') }}</h2>
            <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-300 font-mono">{{ t('home.features.multiAccountDesc') }}</p>
          </div>
          
          <div class="backdrop-blur-md bg-white/40 dark:bg-dark-900/40 border border-gray-200/50 dark:border-dark-800/50 rounded-2xl p-8 hover:border-primary-500/30 dark:hover:border-primary-500/20 shadow-glass transition-all duration-300 hover:-translate-y-1 hover:shadow-glow/5 relative overflow-hidden group">
            <div class="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-transparent via-primary-500/40 to-transparent opacity-0 group-hover:opacity-100 transition-opacity"></div>
            <div class="inline-flex p-3 rounded-xl bg-primary-500/10 text-primary-500 dark:bg-primary-500/20 dark:text-primary-400 mb-5">
              <Icon name="chart" size="lg" />
            </div>
            <h2 class="text-lg font-bold text-gray-950 dark:text-white">{{ t('home.features.balanceQuota') }}</h2>
            <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-300 font-mono">{{ t('home.features.balanceQuotaDesc') }}</p>
          </div>
        </div>
      </section>

      <!-- AI Provider Section -->
      <section class="px-6 py-20 relative">
        <div class="mx-auto max-w-7xl relative z-10">
          <div class="flex flex-col justify-between gap-5 md:flex-row md:items-end mb-10">
            <div>
              <h2 class="text-3xl font-black tracking-tight text-gray-950 dark:text-white">{{ t('home.providers.title') }}</h2>
              <p class="mt-2 text-sm text-gray-500 dark:text-dark-300 font-mono">// {{ t('home.providers.description') }}</p>
            </div>
            <router-link to="/docs" class="inline-flex h-10 items-center justify-center rounded-xl border border-gray-200 bg-white/30 px-4 text-sm font-semibold text-gray-700 hover:text-primary-600 hover:border-primary-500/40 transition-all dark:border-dark-800 dark:bg-dark-900/30 dark:text-dark-200 dark:hover:text-primary-400">
              <Icon name="book" size="sm" class="mr-2" />
              {{ t('home.docs') }}
            </router-link>
          </div>

          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
            <div
              v-for="provider in providers"
              :key="provider.name"
              class="flex items-center gap-4 rounded-xl border border-gray-200/60 bg-white/40 p-5 dark:border-dark-800/80 dark:bg-dark-900/40 backdrop-blur-md hover:border-primary-500/30 dark:hover:border-primary-500/20 shadow-glass-sm hover:-translate-y-0.5 hover:shadow-glow/5 transition-all duration-300 group"
            >
              <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-500/10 text-primary-500 dark:bg-primary-500/20 dark:text-primary-400 text-base font-black font-mono group-hover:scale-110 transition-transform">
                {{ provider.initial }}
              </span>
              <div>
                <div class="text-sm font-bold text-gray-900 dark:text-white">{{ provider.name }}</div>
                <div class="text-[10px] uppercase font-mono font-bold tracking-widest text-primary-500 dark:text-primary-400 mt-0.5">{{ provider.status }}</div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </main>

    <!-- Footer -->
    <footer class="relative z-30 border-t border-gray-200/20 px-6 py-8 bg-white/20 dark:bg-dark-950/20 backdrop-blur-md">
      <div class="mx-auto flex max-w-7xl flex-col items-center justify-between gap-4 text-center sm:flex-row sm:text-left">
        <p class="text-sm text-gray-500 dark:text-dark-400 font-mono">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-5 font-mono">
          <router-link to="/docs" class="text-sm font-medium text-gray-500 transition-colors hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400">
            {{ t('home.docs') }}
          </router-link>
          <a :href="githubUrl" target="_blank" rel="noopener noreferrer" class="text-sm font-medium text-gray-500 transition-colors hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400">
            GitHub
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'LightBridge')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.heroDescription'))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isDark = ref(document.documentElement.classList.contains('dark'))
const githubUrl = 'https://github.com/WilliamWang1721/LightBridge'

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())

const providers = computed(() => [
  { name: t('home.providers.claude'), initial: 'C', status: t('home.providers.supported') },
  { name: 'GPT', initial: 'G', status: t('home.providers.supported') },
  { name: t('home.providers.gemini'), initial: 'G', status: t('home.providers.supported') },
  { name: t('home.providers.antigravity'), initial: 'A', status: t('home.providers.supported') },
  { name: t('home.providers.more'), initial: '+', status: t('home.providers.soon') },
])

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

// ==================== Canvas Particle Background ====================
const particleCanvas = ref<HTMLCanvasElement | null>(null)

interface Particle {
  x: number
  y: number
  vx: number
  vy: number
  radius: number
  alpha: number
}

// ==================== Mock Terminal Animation ====================
const terminalLines = ref<string[]>([])
const isTyping = ref(false)

const playTerminalAnimation = async () => {
  const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
  const typeText = async (text: string) => {
    isTyping.value = true
    let current = '$ '
    terminalLines.value.push(current)
    for (let i = 0; i < text.length; i++) {
      current += text[i]
      terminalLines.value[terminalLines.value.length - 1] = current
      await delay(Math.random() * 40 + 30)
    }
    isTyping.value = false
  }

  while (true) {
    terminalLines.value = []
    await delay(500)
    await typeText('curl -X POST /v1/chat/completions')
    await delay(600)
    terminalLines.value.push('routing: claude ➜ gpt ➜ gemini [load_balanced]')
    await delay(800)
    terminalLines.value.push('HTTP/1.1 200 OK')
    await delay(300)
    
    // Output response snippet
    const responseLines = [
      '{',
      '  "id": "lb-9f3j2d0k",',
      '  "model": "gemini-3.5-flash",',
      '  "choices": [',
      '    { "message": { "content": "Request resolved." } }',
      '  ],',
      '  "usage": { "total_tokens": 142 }',
      '}'
    ]
    for (const l of responseLines) {
      terminalLines.value.push(l)
      await delay(80)
    }
    
    await delay(6000) // Keep output visible
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()

  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }

  // Play Terminal loop
  playTerminalAnimation()

  // Init Canvas Particles
  if (particleCanvas.value) {
    const canvas = particleCanvas.value
    const ctx = canvas.getContext('2d')
    if (ctx) {
      let animationFrameId: number
      let width = (canvas.width = window.innerWidth)
      let height = (canvas.height = window.innerHeight)

      const handleResize = () => {
        if (!canvas) return
        width = canvas.width = window.innerWidth
        height = canvas.height = window.innerHeight
      }
      window.addEventListener('resize', handleResize)

      const particles: Particle[] = []
      const particleCount = Math.min(Math.floor((width * height) / 15000), 80)

      for (let i = 0; i < particleCount; i++) {
        particles.push({
          x: Math.random() * width,
          y: Math.random() * height,
          vx: (Math.random() - 0.5) * 0.4,
          vy: (Math.random() - 0.5) * 0.4,
          radius: Math.random() * 2 + 1,
          alpha: Math.random() * 0.5 + 0.2,
        })
      }

      const mouse = { x: -1000, y: -1000 }
      const handleMouseMove = (e: MouseEvent) => {
        mouse.x = e.clientX
        mouse.y = e.clientY
      }
      const handleMouseLeave = () => {
        mouse.x = -1000
        mouse.y = -1000
      }
      window.addEventListener('mousemove', handleMouseMove)
      window.addEventListener('mouseleave', handleMouseLeave)

      const draw = () => {
        ctx.clearRect(0, 0, width, height)
        const isDarkTheme = document.documentElement.classList.contains('dark')
        const color = isDarkTheme ? '20, 184, 166' : '13, 148, 136' // teal color

        particles.forEach((p) => {
          p.x += p.vx
          p.y += p.vy

          if (p.x < 0 || p.x > width) p.vx *= -1
          if (p.y < 0 || p.y > height) p.vy *= -1

          ctx.beginPath()
          ctx.arc(p.x, p.y, p.radius, 0, Math.PI * 2)
          ctx.fillStyle = `rgba(${color}, ${p.alpha})`
          ctx.fill()
        })

        // Draw connections
        for (let i = 0; i < particles.length; i++) {
          for (let j = i + 1; j < particles.length; j++) {
            const dx = particles[i].x - particles[j].x
            const dy = particles[i].y - particles[j].y
            const dist = Math.sqrt(dx * dx + dy * dy)

            if (dist < 100) {
              ctx.beginPath()
              ctx.moveTo(particles[i].x, particles[i].y)
              ctx.lineTo(particles[j].x, particles[j].y)
              ctx.strokeStyle = `rgba(${color}, ${(1 - dist / 100) * 0.15})`
              ctx.lineWidth = 0.8
              ctx.stroke()
            }
          }
        }

        // Draw connections to mouse
        if (mouse.x !== -1000) {
          particles.forEach((p) => {
            const dx = p.x - mouse.x
            const dy = p.y - mouse.y
            const dist = Math.sqrt(dx * dx + dy * dy)
            if (dist < 150) {
              ctx.beginPath()
              ctx.moveTo(p.x, p.y)
              ctx.lineTo(mouse.x, mouse.y)
              ctx.strokeStyle = `rgba(${color}, ${(1 - dist / 150) * 0.25})`
              ctx.lineWidth = 1
              ctx.stroke()
            }
          })
        }

        animationFrameId = requestAnimationFrame(draw)
      }

      draw()

      onUnmounted(() => {
        window.removeEventListener('resize', handleResize)
        window.removeEventListener('mousemove', handleMouseMove)
        window.removeEventListener('mouseleave', handleMouseLeave)
        cancelAnimationFrame(animationFrameId)
      })
    }
  }
})
</script>

<style scoped>
/* Background grid pattern */
.bg-grid {
  background-image: radial-gradient(rgba(20, 184, 166, 0.05) 1.5px, transparent 1.5px);
  background-size: 24px 24px;
}
:global(.dark) .bg-grid {
  background-image: radial-gradient(rgba(20, 184, 166, 0.12) 1.5px, transparent 1.5px);
}

/* Custom terminal typewriter cursor */
.cursor {
  display: inline-block;
  width: 6px;
  height: 14px;
  background: #14b8a6;
  animation: blink-cursor 1s step-end infinite;
  vertical-align: middle;
}

@keyframes blink-cursor {
  0%, 50% { opacity: 1; }
  51%, 100% { opacity: 0; }
}

/* Fade up transition animations */
@keyframes fade-up-anim {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.fade-up {
  animation: fade-up-anim 0.6s cubic-bezier(0.16, 1, 0.3, 1) forwards;
}

.fade-up-delay-1 {
  animation-delay: 0.15s;
  opacity: 0;
}
</style>
