import type { Config } from 'tailwindcss';
import animate from 'tailwindcss-animate';

// P13.5 · Tailwind theme mirrors jarvis's design-tokens.css so the React
// island visually matches the vanilla pages.
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Status palette aligned with --ds-status-* tokens.
        status: {
          working: '#38BDF8',
          idle: '#64748B',
          meeting: '#A855F7',
          stuck: '#F97316',
          success: '#22C55E',
          warn: '#EAB308',
          error: '#EF4444',
          info: '#3B82F6',
        },
        surface: {
          1: 'rgba(255, 255, 255, 0.025)',
          2: 'rgba(255, 255, 255, 0.04)',
          3: 'rgba(255, 255, 255, 0.06)',
          4: 'rgba(255, 255, 255, 0.10)',
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
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'monospace'],
      },
    },
  },
  plugins: [animate],
} satisfies Config;
