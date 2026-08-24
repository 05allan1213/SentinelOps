import babelParser from '@babel/eslint-parser'
import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'

const asWarnings = (rules) => Object.fromEntries(
  Object.keys(rules).map((rule) => [rule, 'warn']),
)

export default [
  {
    ignores: ['dist', 'node_modules', 'playwright-report', 'test-results'],
  },
  {
    files: ['**/*.{js,ts,tsx}'],
    languageOptions: {
      ecmaVersion: 'latest',
      globals: {
        ...globals.browser,
        ...globals.node,
      },
      parser: babelParser,
      parserOptions: {
        requireConfigFile: false,
        babelOptions: {
          presets: [
            ['@babel/preset-typescript', { ignoreExtensions: true }],
          ],
          plugins: ['@babel/plugin-syntax-jsx'],
        },
      },
      sourceType: 'module',
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...asWarnings(js.configs.recommended.rules),
      ...asWarnings(reactHooks.configs.flat.recommended.rules),
      'no-undef': 'off',
      'no-unused-vars': 'off',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
]
