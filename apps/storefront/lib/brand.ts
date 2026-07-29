/**
 * The platform's name, kept out of the message catalogs on purpose.
 *
 * "Multiticketing" is a brand, not a word: it reads the same in English and in
 * Spanish, and a copy of it sitting in es.json is an invitation for a
 * well-meaning translator to render it. Sentences that mention it interpolate
 * it instead — "Powered by {brand}" — so the sentence is translatable and the
 * name is not.
 */
export const BRAND_NAME = "Multiticketing";
