// ESLint flat config (ESLint 9).
//
// Type-aware linting is deliberately not enabled. `tsc --noEmit` already runs
// in `npm run build` and in `npm run typecheck` with `strict` on, so the rules
// that need a TypeScript program would re-derive the same information at
// several times the cost and report it in a second place. What is left here is
// the class of mistake the compiler accepts: a hook called conditionally, an
// unused variable, a `console.log` left in.
import js from '@eslint/js'
import globals from 'globals'
import tseslint from '@typescript-eslint/eslint-plugin'
import tsparser from '@typescript-eslint/parser'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'

export default [
  {
    // Flat config has no `.eslintignore`; ignores are a config entry, and one
    // with only `ignores` applies globally.
    //
    // routeTree.gen.ts is machine-written and its own header says not to lint
    // it. The rest are build output and dependencies.
    ignores: [
      'dist/**',
      'node_modules/**',
      'src/routeTree.gen.ts',
      'postcss.config.js',
      'tailwind.config.js',
    ],
  },

  js.configs.recommended,

  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      parser: tsparser,
      ecmaVersion: 2020,
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.es2020,
      },
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
    },
    plugins: {
      '@typescript-eslint': tseslint,
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...tseslint.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,

      // An undefined identifier in TypeScript is a compile error, so this rule
      // has nothing left to catch — and it cannot see type-only globals, so it
      // reports `RequestCredentials` or `RequestInit` as undefined in code that
      // type-checks cleanly. Disabling it on TS is typescript-eslint's own
      // recommendation.
      'no-undef': 'off',

      // The base rule does not understand type-only syntax and reports
      // false positives on interfaces and overloads; the TS-aware one
      // replaces it.
      'no-unused-vars': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        // A leading underscore is the established way to say "required by the
        // signature, deliberately unused" — for instance the `_data` in a
        // TanStack Query `onSuccess(_data, id)`.
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],

      // Fast Refresh can only replace a module whose exports are all
      // components. A helper exported beside one silently degrades every edit
      // in that file to a full reload.
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],

      // console.error survives: client.ts logs schema mismatches there on
      // purpose, because a contract break is something a developer needs to
      // see in the console. A stray console.log is different — it is debugging
      // residue, and `--max-warnings 0` means it fails the run.
      'no-console': ['warn', { allow: ['warn', 'error'] }],
    },
  },

  {
    // Node context: config files and anything under src/test run outside the
    // browser, so browser globals are the wrong set and `console` is fine.
    files: ['*.config.{ts,js}', 'src/test/**/*.ts'],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      'no-console': 'off',
    },
  },

  {
    // Test files run in both worlds: jsdom supplies the browser globals, the
    // runner supplies node's. `globals.vitest` covers describe/it/expect, which
    // are ambient because the Vitest config sets `globals: true`.
    files: ['**/*.test.{ts,tsx}', 'src/test/**/*.{ts,tsx}'],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node, ...globals.vitest },
    },
    rules: {
      'no-console': 'off',
    },
  },
]
