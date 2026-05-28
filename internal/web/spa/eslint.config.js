// Flat eslint config (eslint 9 default).
// Keeps lint fast by not requiring type info (no `project` parser
// option), which means rules like @typescript-eslint/no-unsafe-* are
// off — those would need recommended-type-checked. The rules we do
// enable catch the actual bug classes that cost us time: rules-of-
// hooks, exhaustive-deps, unused vars.

import js from '@eslint/js';
import tsParser from '@typescript-eslint/parser';
import tsPlugin from '@typescript-eslint/eslint-plugin';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';

export default [
  {
    ignores: [
      'node_modules/**',
      'dist/**',
      '../static/public/react/**',
      'eslint.config.js',
      'postcss.config.js',
      'tailwind.config.ts',
      'vite.config.ts',
    ],
  },
  js.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: 2022,
        sourceType: 'module',
        ecmaFeatures: { jsx: true },
      },
      globals: {
        ...globals.browser,
      },
    },
    plugins: {
      '@typescript-eslint': tsPlugin,
      react,
      'react-hooks': reactHooks,
    },
    settings: {
      react: { version: '19.0' },
    },
    rules: {
      // TS handles undef + redeclare; turning them off avoids
      // false positives on type-only identifiers.
      'no-undef': 'off',
      'no-redeclare': 'off',
      'no-unused-vars': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-non-null-assertion': 'warn',
      // New JSX transform makes these imports implicit.
      'react/jsx-uses-react': 'off',
      'react/react-in-jsx-scope': 'off',
      // Hooks discipline — the rules we actually need.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      // Misc correctness.
      'no-empty': ['error', { allowEmptyCatch: true }],
      'no-debugger': 'error',
      'no-console': ['warn', { allow: ['warn', 'error'] }],
    },
  },
];
