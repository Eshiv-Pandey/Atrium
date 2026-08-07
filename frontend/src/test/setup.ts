// Vitest setup, applied to every test file.
//
// Two things are wired here rather than per file. jest-dom's matchers, because
// a test that has to remember to import them is a test that will one day assert
// `expect(el).toBeVisible` as a truthy function reference and pass while
// checking nothing. And the msw server lifecycle, because the interesting
// failure mode of request mocking is a handler leaking from one test into the
// next — resetting between tests is what makes each file independent of the
// order the others ran in.
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, vi } from 'vitest'
import { server } from './server'

// jsdom has no layout, so it implements scrollTo as a thrower rather than a
// no-op. The router's scroll restoration calls it on every navigation, which
// puts a stack trace in the output of any test that renders a route — and a
// suite whose passing output is full of stack traces is one where a real error
// goes unnoticed.
beforeAll(() => {
  vi.stubGlobal('scrollTo', vi.fn())

  // `error` rather than the default `warn`: an unhandled request means the test
  // is talking to something it did not declare, and the assertion that follows
  // would be about a network failure instead of the behaviour under test.
  server.listen({ onUnhandledRequest: 'error' })
})

afterEach(() => {
  server.resetHandlers()
  // Unmounts anything rendered. Without it, a component from a previous test is
  // still in the document and `getByRole` finds two matching elements.
  cleanup()
})

afterAll(() => {
  server.close()
})
