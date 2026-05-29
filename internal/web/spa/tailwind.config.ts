import type { Config } from 'tailwindcss';
import animate from 'tailwindcss-animate';

// Tailwind theme reads its colors from src/styles/tokens.css via RGB
// channel triplets, so tokens.css is the SINGLE SOURCE OF TRUTH for the
// palette (web + the mobile mirror). Alpha utilities like
// `bg-status-working/15` keep working because Tailwind substitutes
// <alpha-value> into the rgb(var(--x-rgb) / <alpha-value>) template at
// build time — this requires the vars to be space-separated channels.
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        accent: {
          DEFAULT: 'rgb(var(--accent-rgb) / <alpha-value>)',
          strong: 'rgb(var(--accent-strong-rgb) / <alpha-value>)',
        },
        // Status palette — token-driven, replaces the old hardcoded
        // (and divergent) hex values. StatusPill now matches AgentCard.
        status: {
          working: 'rgb(var(--status-working-rgb) / <alpha-value>)',
          idle: 'rgb(var(--status-idle-rgb) / <alpha-value>)',
          meeting: 'rgb(var(--status-meeting-rgb) / <alpha-value>)',
          stuck: 'rgb(var(--status-stuck-rgb) / <alpha-value>)',
          success: 'rgb(var(--status-success-rgb) / <alpha-value>)',
          warn: 'rgb(var(--status-warn-rgb) / <alpha-value>)',
          error: 'rgb(var(--status-error-rgb) / <alpha-value>)',
          info: 'rgb(var(--status-info-rgb) / <alpha-value>)',
        },
        surface: {
          1: 'rgb(var(--surface-tint-rgb) / 0.025)',
          2: 'rgb(var(--surface-tint-rgb) / 0.04)',
          3: 'rgb(var(--surface-tint-rgb) / 0.06)',
          4: 'rgb(var(--surface-tint-rgb) / 0.1)',
        },
      },
      borderRadius: {
        xs: '4px',
        sm: '6px',
        md: '8px',
        lg: '12px',
        xl: '16px',
        '2xl': '24px',
      },
      fontFamily: {
        sans: ['Inter Variable', 'Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono Variable', 'JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      boxShadow: {
        'glow-sm': 'var(--glow-violet-sm)',
        'glow-md': 'var(--glow-violet-md)',
        'glow-lg': 'var(--glow-violet-lg)',
        'glow-success': 'var(--glow-success)',
        'glow-error': 'var(--glow-error)',
        'elev-1': 'var(--elev-1)',
        'elev-2': 'var(--elev-2)',
        'elev-3': 'var(--elev-3)',
      },
      backdropBlur: {
        glass: 'var(--glass-blur)',
        'glass-strong': 'var(--glass-blur-strong)',
      },
      keyframes: {
        'aurora-drift': {
          '0%,100%': { transform: 'translate3d(0,0,0) scale(1)' },
          '33%': { transform: 'translate3d(2%, -2%, 0) scale(1.05)' },
          '66%': { transform: 'translate3d(-2%, 1%, 0) scale(0.97)' },
        },
        shimmer: {
          '100%': { transform: 'translateX(100%)' },
        },
        'glow-pulse': {
          '0%,100%': { boxShadow: 'var(--glow-violet-sm)' },
          '50%': { boxShadow: 'var(--glow-violet-md)' },
        },
      },
      animation: {
        'aurora-drift': 'aurora-drift var(--aurora-drift-duration) ease-in-out infinite',
        shimmer: 'shimmer var(--shimmer-duration) infinite',
        'glow-pulse': 'glow-pulse 2.8s ease-in-out infinite',
      },
    },
  },
  plugins: [animate],
} satisfies Config;
