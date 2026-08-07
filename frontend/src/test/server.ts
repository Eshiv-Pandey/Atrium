// The msw server every test shares.
//
// Deliberately started with no handlers: a default set would mean a test could
// pass because of a response it never declared, and the reader of that test
// would have to go looking in this file to understand what it actually
// exercised. Each test declares the endpoints it cares about with
// `server.use(...)`, and `onUnhandledRequest: 'error'` in the setup file turns
// anything undeclared into a visible failure rather than a silent 404.
//
// Mocking at the network boundary rather than stubbing `fetch` is the point:
// `api.get` builds the URL, sets the headers, sends credentials, reads the
// status, and parses the envelope. A stubbed fetch would replace all of that
// with an assumption about how it behaves. Here the real code runs and only the
// wire is faked.
import { setupServer } from 'msw/node'

export const server = setupServer()

/** The prefix every request in api/client.ts is built against. */
export const API = 'http://localhost/api'
