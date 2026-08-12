import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// 禁止在 className 里直接用 Tailwind 调色板类。状态色一律走
// success / warning / danger / info,中性色走 foreground / muted-foreground /
// border / accent。这条规则存在的意义是防止 token 层被逐步绕开。
const PALETTE_HUES = [
  'slate', 'gray', 'zinc', 'neutral', 'stone',
  'red', 'orange', 'amber', 'yellow', 'lime', 'green', 'emerald', 'teal',
  'cyan', 'sky', 'blue', 'indigo', 'violet', 'purple', 'fuchsia', 'pink', 'rose',
].join('|')
const PRIMITIVE_COLOR = String.raw`(text|bg|border|ring|fill|stroke|from|via|to|decoration|outline|divide|placeholder)-(${PALETTE_HUES})-\d{2,3}`

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: `Literal[value=/${PRIMITIVE_COLOR}/]`,
          message: '不要直接用 Tailwind 调色板类,改用语义 token:success / warning / danger / info / muted-foreground / border。',
        },
        {
          selector: `TemplateElement[value.raw=/${PRIMITIVE_COLOR}/]`,
          message: '不要直接用 Tailwind 调色板类,改用语义 token:success / warning / danger / info / muted-foreground / border。',
        },
      ],
    },
  },
])
