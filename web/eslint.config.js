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

// 禁止 t('key', '中文兜底')。兜底本身就是问题:key 缺失时它会静默地把中文
// 漏给英文用户,而不是暴露出来。曾经因此让英文界面的筛选按钮显示「筛选」。
// 不传兜底的话,缺 key 会直接渲染成 key 本身,一眼就能看见。
const CJK_LITERAL = String.raw`[一-鿿　-〿＀-￯]`

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
        {
          selector: `CallExpression[callee.name='t'] > Literal[value=/${CJK_LITERAL}/]`,
          message: '不要给 t() 传中文兜底:key 缺失时它会静默漏中文给英文用户。把文案加进 locales,只传 key。',
        },
      ],
    },
  },
])
