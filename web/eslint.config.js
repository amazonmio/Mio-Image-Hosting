import vue from 'eslint-plugin-vue'
import ts from 'typescript-eslint'
import prettier from 'eslint-config-prettier'

export default [
  { ignores: ['dist/**', 'node_modules/**', '*.d.ts'] },
  ...vue.configs['flat/essential'],
  {
    files: ['src/**/*.ts', 'vite.config.ts'],
    languageOptions: { parser: ts.parser },
  },
  {
    files: ['src/**/*.vue'],
    languageOptions: { parserOptions: { parser: ts.parser } },
  },
  prettier,
  {
    files: ['src/**/*.{ts,vue}', 'vite.config.ts'],
    rules: {
      curly: ['error', 'all'],
      'one-var': ['error', 'never'],
      'no-nested-ternary': 'error',
      'no-var': 'error',
      'prefer-const': 'error',
      // Intentional `value == null` handles both null and undefined.
      eqeqeq: ['error', 'always', { null: 'ignore' }],
    },
  },
]
