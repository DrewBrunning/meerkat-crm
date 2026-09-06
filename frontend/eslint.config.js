import jsxA11y from 'eslint-plugin-jsx-a11y';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import security from 'eslint-plugin-security';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: [
      'build/**',
      'dist/**',
      'node_modules/**',
      'playwright-report/**',
      'test-results/**',
      'coverage/**',
      // Issue #476 (WEB-02): the sw-upgrade suite generates real production
      // builds under e2e/sw-upgrade/fixtures and its own Playwright artifacts.
      'e2e/sw-upgrade/fixtures/**',
      'playwright-report-sw/**',
      'test-results-sw/**',
    ],
  },
  tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    plugins: {
      'react-hooks': reactHooks,
      react,
      security,
      'jsx-a11y': jsxA11y,
    },
    rules: {
      // WCAG audit (#148) gate: jsx-a11y catches the authoring-time class of
      // findings (unnamed icon buttons, unlabeled controls) the same way
      // TypeScript catches types. Recommended config, minus the rules
      // downgraded below — never disable the plugin outright.
      ...jsxA11y.configs.recommended.rules,
      // The recommended config only checks controls whose role is alert or
      // dialog, and only raw DOM elements — neither of which covers MUI's
      // <IconButton>/<Button> components, so it would NOT have caught the
      // audit's unnamed AppBar buttons / dialog close / info-popover triggers.
      // Extend includeRoles to button/link (their implicit roles) and list the
      // MUI control components as controlComponents. `*Icon` must be listed
      // too: mayHaveAccessibleLabel assumes any React-component child could be
      // a label, so without it a bare <EditIcon/> child makes the rule treat
      // the button as labeled. The 36 findings this surfaced were all genuine.
      'jsx-a11y/control-has-associated-label': [
        'error',
        {
          includeRoles: ['alert', 'dialog', 'button', 'link'],
          controlComponents: ['IconButton', 'Button', '*Icon', '*IconButton'],
        },
      ],
      // Deliberately `warn`, not `error`: all 20 existing hits are
      // interaction-triggered focus (the first field of a just-opened dialog,
      // or a just-activated inline edit), which is correct keyboard a11y —
      // the WCAG concern is *page-load* autofocus, and none of these are
      // that. Kept visible so new page-load autofocus is reviewed, matching
      // the repo's pattern for exhaustive-deps / no-explicit-any.
      'jsx-a11y/no-autofocus': 'warn',
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      // Browser XSS / tabnabbing guards.
      'react/no-danger': 'error',
      'react/jsx-no-target-blank': 'error',
      'security/detect-eval-with-expression': 'error',
      'security/detect-non-literal-regexp': 'error',
      'security/detect-unsafe-regex': 'error',
      // The api/ layer parses responses as Record<string, any>; tightening it
      // is a separate refactor. Keep `any` visible (warning) but non-blocking.
      '@typescript-eslint/no-explicit-any': 'warn',
      // `_`-prefixed bindings are deliberately ignored (destructure-and-ignore).
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
    },
  },
  {
    // Playwright specs legitimately build RegExps from variables in assertions.
    files: ['e2e/**'],
    rules: {
      'security/detect-non-literal-regexp': 'off',
    },
  },
  {
    // fixtures.ts's `page` fixture override (issue #259's automatic a11y
    // scan) has a `(fixtures, use, testInfo) => {...}` signature per
    // Playwright's own fixture convention. eslint-plugin-react-hooks
    // pattern-matches any function param literally named `use` as a React
    // Hook call and misfires rules-of-hooks on it — this file has no JSX and
    // no actual React hooks, so the rule has nothing real to protect here.
    files: ['e2e/fixtures.ts'],
    rules: {
      'react-hooks/rules-of-hooks': 'off',
    },
  },
);
