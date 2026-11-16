// Shared page size for the global explorer so the initial SSR page and each
// client "Load more" fetch request the same number of cards.
//
// Its own module rather than lib/api.ts, which only the server may import: the
// "Load more" button is a client component, and lib/api.ts carries the Sentry
// SDK (for the trace headers), which must stay out of the browser's first load.
export const EXPLORER_PAGE_SIZE = 12;
