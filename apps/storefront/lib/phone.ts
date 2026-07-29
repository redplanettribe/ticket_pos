/**
 * The Customer phone number as the checkout dialog and "My info" see it (#103,
 * #105): the country table behind the dialling-code selector, a *mirror* of the
 * API's validation, and the split that turns a stored number back into the two
 * halves a form shows.
 *
 * Mirror is the operative word. `backend/internal/platform/phone.go` is the
 * single source of truth for what counts as a valid phone number, and the
 * server's verdict is the one that decides whether a Sale is recorded. What
 * lives here exists only so a buyer who fat-fingers a digit is told immediately
 * instead of after a round trip — so it must never be *stricter* than the
 * backend (which would lock out a legitimate buyer the API would have accepted),
 * and when the two disagree the field error the API returns wins on screen. The
 * case tables in phone.test.ts and phone_test.go are kept in step for exactly
 * this reason.
 *
 * The field is optional throughout (#103). A buyer who skips it sees the
 * checkout they see today and types their number on PayPhone's form instead; a
 * buyer who fills it gets a hosted form with nothing left but the card. Nothing
 * here ever invents a value — PayPhone's rules prohibit static or filler
 * cardholder data — so a blank field normalises to null, never to a placeholder.
 *
 * No phone-number library, here or on the server. Per-country national formats
 * are a moving target, and a client rule stricter than PayPhone's own costs a
 * sale.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import { DEFAULT_LOCALE, type Locale } from "./format.ts";

/**
 * A row of the country selector as it is rendered: the region it stands for, the
 * name a buyer looks for, and the dialling code that gets prepended to what they
 * type.
 *
 * `name` is a rendering of `regionCode` under one Locale and nothing more, so it
 * is the wrong thing to key, store or compare on. `regionCode` is the identity —
 * the same row is "Germany" and "Alemania" depending only on who is reading.
 */
export type Country = {
  regionCode: string;
  name: string;
  diallingCode: string;
};

/** Ecuador — the default selection, and the one strict validation tier. */
export const ECUADOR_REGION_CODE = "EC";
export const ECUADOR_DIALLING_CODE = "+593";

/**
 * The country table: an ISO 3166-1 alpha-2 region and the dialling code you
 * reach it on.
 *
 * Only the dialling codes are kept by hand, because a dialling code is a fact
 * about the telephone network that no platform API will tell us. The NAMES are
 * not kept here at all — they come from `Intl.DisplayNames` under the Locale the
 * page was routed with (see `countries` below), so the Spanish Storefront gets
 * "Alemania" without anyone translating two hundred rows, and a renaming like
 * Turkey's arrives on its own. What was here before was two hundred English
 * strings that served a Spanish reader nothing and that only a hand-edit could
 * ever correct.
 *
 * It carries NO validation duty whatsoever: it supplies the prefix for display
 * and for assembling the canonical number, and that is all. Validation is the
 * two tiers in `normalizePhone` below, which key off the dialling code in the
 * number itself, not off this list. Adding or removing a row changes what the
 * selector offers and nothing about what is accepted.
 *
 * Ordered by region code, which is the only order that is the same in every
 * language — what a buyer sees is sorted per Locale by `countries`, and a source
 * file sorted by yesterday's English names would just be a lie about the order
 * anyone reads it in.
 *
 * Accepted cosmetic imperfection (#103): `+1` covers the United States, Canada
 * and some twenty other countries, and `+7` covers Russia and Kazakhstan. The
 * longest-prefix match in `splitPhone` therefore resolves a stored `+1` number
 * to a dialling code shared by several rows, and the selector lands on whichever
 * of them the reader's Locale sorts first. This is deliberately not solved: the
 * value sent to PayPhone is unaffected, split dialling-code columns would not
 * have recorded the distinction either, and the alternative is a second table of
 * area-code ranges maintained forever. The Caribbean NANP territories are listed
 * with their full three-digit `+1XXX` codes, so those at least resolve exactly.
 */
const DIALLING_CODES: readonly { regionCode: string; diallingCode: string }[] = [
  { regionCode: "AD", diallingCode: "+376" },
  { regionCode: "AE", diallingCode: "+971" },
  { regionCode: "AF", diallingCode: "+93" },
  { regionCode: "AG", diallingCode: "+1268" },
  { regionCode: "AL", diallingCode: "+355" },
  { regionCode: "AM", diallingCode: "+374" },
  { regionCode: "AO", diallingCode: "+244" },
  { regionCode: "AR", diallingCode: "+54" },
  { regionCode: "AT", diallingCode: "+43" },
  { regionCode: "AU", diallingCode: "+61" },
  { regionCode: "AW", diallingCode: "+297" },
  { regionCode: "AZ", diallingCode: "+994" },
  { regionCode: "BA", diallingCode: "+387" },
  { regionCode: "BB", diallingCode: "+1246" },
  { regionCode: "BD", diallingCode: "+880" },
  { regionCode: "BE", diallingCode: "+32" },
  { regionCode: "BF", diallingCode: "+226" },
  { regionCode: "BG", diallingCode: "+359" },
  { regionCode: "BH", diallingCode: "+973" },
  { regionCode: "BI", diallingCode: "+257" },
  { regionCode: "BJ", diallingCode: "+229" },
  { regionCode: "BM", diallingCode: "+1441" },
  { regionCode: "BN", diallingCode: "+673" },
  { regionCode: "BO", diallingCode: "+591" },
  { regionCode: "BR", diallingCode: "+55" },
  { regionCode: "BS", diallingCode: "+1242" },
  { regionCode: "BT", diallingCode: "+975" },
  { regionCode: "BW", diallingCode: "+267" },
  { regionCode: "BY", diallingCode: "+375" },
  { regionCode: "BZ", diallingCode: "+501" },
  { regionCode: "CA", diallingCode: "+1" },
  { regionCode: "CD", diallingCode: "+243" },
  { regionCode: "CF", diallingCode: "+236" },
  { regionCode: "CG", diallingCode: "+242" },
  { regionCode: "CH", diallingCode: "+41" },
  { regionCode: "CI", diallingCode: "+225" },
  { regionCode: "CL", diallingCode: "+56" },
  { regionCode: "CM", diallingCode: "+237" },
  { regionCode: "CN", diallingCode: "+86" },
  { regionCode: "CO", diallingCode: "+57" },
  { regionCode: "CR", diallingCode: "+506" },
  { regionCode: "CU", diallingCode: "+53" },
  { regionCode: "CV", diallingCode: "+238" },
  { regionCode: "CW", diallingCode: "+599" },
  { regionCode: "CY", diallingCode: "+357" },
  { regionCode: "CZ", diallingCode: "+420" },
  { regionCode: "DE", diallingCode: "+49" },
  { regionCode: "DJ", diallingCode: "+253" },
  { regionCode: "DK", diallingCode: "+45" },
  { regionCode: "DM", diallingCode: "+1767" },
  { regionCode: "DO", diallingCode: "+1809" },
  { regionCode: "DZ", diallingCode: "+213" },
  { regionCode: "EC", diallingCode: ECUADOR_DIALLING_CODE },
  { regionCode: "EE", diallingCode: "+372" },
  { regionCode: "EG", diallingCode: "+20" },
  { regionCode: "ER", diallingCode: "+291" },
  { regionCode: "ES", diallingCode: "+34" },
  { regionCode: "ET", diallingCode: "+251" },
  { regionCode: "FI", diallingCode: "+358" },
  { regionCode: "FJ", diallingCode: "+679" },
  { regionCode: "FM", diallingCode: "+691" },
  { regionCode: "FR", diallingCode: "+33" },
  { regionCode: "GA", diallingCode: "+241" },
  { regionCode: "GB", diallingCode: "+44" },
  { regionCode: "GD", diallingCode: "+1473" },
  { regionCode: "GE", diallingCode: "+995" },
  { regionCode: "GF", diallingCode: "+594" },
  { regionCode: "GH", diallingCode: "+233" },
  { regionCode: "GI", diallingCode: "+350" },
  { regionCode: "GL", diallingCode: "+299" },
  { regionCode: "GM", diallingCode: "+220" },
  { regionCode: "GN", diallingCode: "+224" },
  { regionCode: "GP", diallingCode: "+590" },
  { regionCode: "GQ", diallingCode: "+240" },
  { regionCode: "GR", diallingCode: "+30" },
  { regionCode: "GT", diallingCode: "+502" },
  { regionCode: "GU", diallingCode: "+1671" },
  { regionCode: "GW", diallingCode: "+245" },
  { regionCode: "GY", diallingCode: "+592" },
  { regionCode: "HK", diallingCode: "+852" },
  { regionCode: "HN", diallingCode: "+504" },
  { regionCode: "HR", diallingCode: "+385" },
  { regionCode: "HT", diallingCode: "+509" },
  { regionCode: "HU", diallingCode: "+36" },
  { regionCode: "ID", diallingCode: "+62" },
  { regionCode: "IE", diallingCode: "+353" },
  { regionCode: "IL", diallingCode: "+972" },
  { regionCode: "IN", diallingCode: "+91" },
  { regionCode: "IQ", diallingCode: "+964" },
  { regionCode: "IR", diallingCode: "+98" },
  { regionCode: "IS", diallingCode: "+354" },
  { regionCode: "IT", diallingCode: "+39" },
  { regionCode: "JM", diallingCode: "+1876" },
  { regionCode: "JO", diallingCode: "+962" },
  { regionCode: "JP", diallingCode: "+81" },
  { regionCode: "KE", diallingCode: "+254" },
  { regionCode: "KG", diallingCode: "+996" },
  { regionCode: "KH", diallingCode: "+855" },
  { regionCode: "KI", diallingCode: "+686" },
  { regionCode: "KM", diallingCode: "+269" },
  { regionCode: "KN", diallingCode: "+1869" },
  { regionCode: "KP", diallingCode: "+850" },
  { regionCode: "KR", diallingCode: "+82" },
  { regionCode: "KW", diallingCode: "+965" },
  { regionCode: "KY", diallingCode: "+1345" },
  { regionCode: "KZ", diallingCode: "+7" },
  { regionCode: "LA", diallingCode: "+856" },
  { regionCode: "LB", diallingCode: "+961" },
  { regionCode: "LC", diallingCode: "+1758" },
  { regionCode: "LI", diallingCode: "+423" },
  { regionCode: "LK", diallingCode: "+94" },
  { regionCode: "LR", diallingCode: "+231" },
  { regionCode: "LS", diallingCode: "+266" },
  { regionCode: "LT", diallingCode: "+370" },
  { regionCode: "LU", diallingCode: "+352" },
  { regionCode: "LV", diallingCode: "+371" },
  { regionCode: "LY", diallingCode: "+218" },
  { regionCode: "MA", diallingCode: "+212" },
  { regionCode: "MC", diallingCode: "+377" },
  { regionCode: "MD", diallingCode: "+373" },
  { regionCode: "ME", diallingCode: "+382" },
  { regionCode: "MG", diallingCode: "+261" },
  { regionCode: "MH", diallingCode: "+692" },
  { regionCode: "MK", diallingCode: "+389" },
  { regionCode: "ML", diallingCode: "+223" },
  { regionCode: "MM", diallingCode: "+95" },
  { regionCode: "MN", diallingCode: "+976" },
  { regionCode: "MO", diallingCode: "+853" },
  { regionCode: "MQ", diallingCode: "+596" },
  { regionCode: "MR", diallingCode: "+222" },
  { regionCode: "MT", diallingCode: "+356" },
  { regionCode: "MU", diallingCode: "+230" },
  { regionCode: "MV", diallingCode: "+960" },
  { regionCode: "MW", diallingCode: "+265" },
  { regionCode: "MX", diallingCode: "+52" },
  { regionCode: "MY", diallingCode: "+60" },
  { regionCode: "MZ", diallingCode: "+258" },
  { regionCode: "NA", diallingCode: "+264" },
  { regionCode: "NC", diallingCode: "+687" },
  { regionCode: "NE", diallingCode: "+227" },
  { regionCode: "NG", diallingCode: "+234" },
  { regionCode: "NI", diallingCode: "+505" },
  { regionCode: "NL", diallingCode: "+31" },
  { regionCode: "NO", diallingCode: "+47" },
  { regionCode: "NP", diallingCode: "+977" },
  { regionCode: "NR", diallingCode: "+674" },
  { regionCode: "NZ", diallingCode: "+64" },
  { regionCode: "OM", diallingCode: "+968" },
  { regionCode: "PA", diallingCode: "+507" },
  { regionCode: "PE", diallingCode: "+51" },
  { regionCode: "PF", diallingCode: "+689" },
  { regionCode: "PG", diallingCode: "+675" },
  { regionCode: "PH", diallingCode: "+63" },
  { regionCode: "PK", diallingCode: "+92" },
  { regionCode: "PL", diallingCode: "+48" },
  { regionCode: "PR", diallingCode: "+1787" },
  { regionCode: "PS", diallingCode: "+970" },
  { regionCode: "PT", diallingCode: "+351" },
  { regionCode: "PW", diallingCode: "+680" },
  { regionCode: "PY", diallingCode: "+595" },
  { regionCode: "QA", diallingCode: "+974" },
  { regionCode: "RE", diallingCode: "+262" },
  { regionCode: "RO", diallingCode: "+40" },
  { regionCode: "RS", diallingCode: "+381" },
  { regionCode: "RU", diallingCode: "+7" },
  { regionCode: "RW", diallingCode: "+250" },
  { regionCode: "SA", diallingCode: "+966" },
  { regionCode: "SB", diallingCode: "+677" },
  { regionCode: "SC", diallingCode: "+248" },
  { regionCode: "SD", diallingCode: "+249" },
  { regionCode: "SE", diallingCode: "+46" },
  { regionCode: "SG", diallingCode: "+65" },
  { regionCode: "SI", diallingCode: "+386" },
  { regionCode: "SK", diallingCode: "+421" },
  { regionCode: "SL", diallingCode: "+232" },
  { regionCode: "SM", diallingCode: "+378" },
  { regionCode: "SN", diallingCode: "+221" },
  { regionCode: "SO", diallingCode: "+252" },
  { regionCode: "SR", diallingCode: "+597" },
  { regionCode: "SS", diallingCode: "+211" },
  { regionCode: "ST", diallingCode: "+239" },
  { regionCode: "SV", diallingCode: "+503" },
  { regionCode: "SX", diallingCode: "+1721" },
  { regionCode: "SY", diallingCode: "+963" },
  { regionCode: "SZ", diallingCode: "+268" },
  { regionCode: "TC", diallingCode: "+1649" },
  { regionCode: "TD", diallingCode: "+235" },
  { regionCode: "TG", diallingCode: "+228" },
  { regionCode: "TH", diallingCode: "+66" },
  { regionCode: "TJ", diallingCode: "+992" },
  { regionCode: "TL", diallingCode: "+670" },
  { regionCode: "TM", diallingCode: "+993" },
  { regionCode: "TN", diallingCode: "+216" },
  { regionCode: "TO", diallingCode: "+676" },
  { regionCode: "TR", diallingCode: "+90" },
  { regionCode: "TT", diallingCode: "+1868" },
  { regionCode: "TV", diallingCode: "+688" },
  { regionCode: "TW", diallingCode: "+886" },
  { regionCode: "TZ", diallingCode: "+255" },
  { regionCode: "UA", diallingCode: "+380" },
  { regionCode: "UG", diallingCode: "+256" },
  { regionCode: "US", diallingCode: "+1" },
  { regionCode: "UY", diallingCode: "+598" },
  { regionCode: "UZ", diallingCode: "+998" },
  { regionCode: "VA", diallingCode: "+379" },
  { regionCode: "VC", diallingCode: "+1784" },
  { regionCode: "VE", diallingCode: "+58" },
  { regionCode: "VG", diallingCode: "+1284" },
  { regionCode: "VI", diallingCode: "+1340" },
  { regionCode: "VN", diallingCode: "+84" },
  { regionCode: "VU", diallingCode: "+678" },
  { regionCode: "WS", diallingCode: "+685" },
  { regionCode: "XK", diallingCode: "+383" },
  { regionCode: "YE", diallingCode: "+967" },
  { regionCode: "ZA", diallingCode: "+27" },
  { regionCode: "ZM", diallingCode: "+260" },
  { regionCode: "ZW", diallingCode: "+263" },
];

/**
 * The Intl.DisplayNames of a Locale, built once. Constructing one is the
 * expensive part — it loads a locale's whole region table — and both pickers
 * rebuild their list on every keystroke that re-renders the form.
 */
const DISPLAY_NAMES = new Map<Locale, Intl.DisplayNames>();

function displayNames(locale: Locale): Intl.DisplayNames {
  let names = DISPLAY_NAMES.get(locale);
  if (!names) {
    // "none" rather than the default "code": a missing name must come back as
    // nothing so the fallback below can try elsewhere, instead of arriving
    // disguised as the string "ZX".
    names = new Intl.DisplayNames(locale, { type: "region", fallback: "none" });
    DISPLAY_NAMES.set(locale, names);
  }
  return names;
}

function lookupName(regionCode: string, locale: Locale): string | undefined {
  try {
    // `of` throws on anything that is not a well-formed region subtag, which a
    // typo in the table above would be. A selector row is not worth a crashed
    // checkout, so it degrades to the next candidate instead.
    return displayNames(locale).of(regionCode) || undefined;
  } catch {
    return undefined;
  }
}

/**
 * What to call a region under a Locale.
 *
 * Three candidates, in order: the Locale asked for, then English, then the
 * region code itself. The English step is what covers a runtime whose ICU data
 * was trimmed to one language — the name is then in the wrong language, which is
 * a blemish, where a blank option in the middle of the selector is a row nobody
 * can pick. The code is the last resort for a region CLDR has never heard of; no
 * row in the table above reaches it, and phone.test.ts holds that line.
 */
export function countryName(regionCode: string, locale: Locale = DEFAULT_LOCALE): string {
  return lookupName(regionCode, locale) ?? lookupName(regionCode, DEFAULT_LOCALE) ?? regionCode;
}

/** Built lists, per Locale. Two hundred lookups and a collation sort. */
const COUNTRY_LISTS = new Map<Locale, readonly Country[]>();

/**
 * The selector's rows, named and ordered for one Locale.
 *
 * Ecuador is first because it is the default selection and the overwhelming
 * majority of buyers — the common case should need no interaction at all.
 *
 * The rest are sorted by `Intl.Collator`, not by `<`. Comparing the strings
 * directly is an ASCII ordering wearing alphabetical clothes: it files every
 * accented name after "Z", so a Spanish list would end in a tail of Á-, É- and
 * Ñ- countries that a buyer scanning for "Alemania" would never reach. The
 * collator is also the only thing that knows a language's own rules about where
 * its letters go.
 *
 * The result is plain serialisable data, so a Server Component can build it once
 * and hand it to a client picker as a prop — which is also the only way to be
 * sure the two agree, since a browser's CLDR and the server's are different
 * builds and can disagree on a name ("Turkey" became "Türkiye" in CLDR 42).
 */
export function countries(locale: Locale = DEFAULT_LOCALE): readonly Country[] {
  const cached = COUNTRY_LISTS.get(locale);
  if (cached) return cached;

  const collator = new Intl.Collator(locale);
  const named = DIALLING_CODES.map((row) => ({
    regionCode: row.regionCode,
    name: countryName(row.regionCode, locale),
    diallingCode: row.diallingCode,
  }));

  const ecuador = named.filter((country) => country.regionCode === ECUADOR_REGION_CODE);
  const rest = named
    .filter((country) => country.regionCode !== ECUADOR_REGION_CODE)
    .sort((a, b) => collator.compare(a.name, b.name));

  const list: readonly Country[] = [...ecuador, ...rest];
  COUNTRY_LISTS.set(locale, list);
  return list;
}

/**
 * The two field messages, one per tier, worded exactly as the API words them so
 * the field does not visibly change its mind when the server answers.
 * Mirrors PhoneEcuadorMessage and PhoneGenericMessage in phone.go.
 */
export const PHONE_ECUADOR_MESSAGE = "must be an Ecuadorian mobile: 9 digits starting with 9";
export const PHONE_GENERIC_MESSAGE = "must be 4–15 digits in international format, like +12025550123";

/**
 * The punctuation people write phone numbers with — "+593 (0)98-765.4321" — all
 * of which is dropped. Anything else that is not a digit is a typo, and is
 * rejected rather than silently reinterpreted into a different number.
 */
const PHONE_PUNCTUATION = /[\s\-().\/]/g;

/** Ecuador's dialling code as bare digits — the strict tier's discriminator. */
const ECUADOR_DIGITS = ECUADOR_DIALLING_CODE.slice(1);

/**
 * scanPhone strips that punctuation and returns the bare digits without the
 * leading plus, or null for anything that is not a plausible international
 * number: a missing plus, a plus that is not first, a stray character, or no
 * digits at all. Mirrors scanPhone in phone.go, including its ASCII-only
 * reading of "digit" — a fullwidth or Arabic-Indic digit is not something to
 * guess at.
 */
function scanPhone(phone: string): string | null {
  const stripped = phone.replace(PHONE_PUNCTUATION, "");
  if (!/^\+[0-9]+$/.test(stripped)) return null;
  return stripped.slice(1);
}

/**
 * normalizePhone applies the rule and returns the canonical E.164 form —
 * "+593987654321", a leading plus and nothing but digits — or null when the
 * number does not pass. That canonical string is what is sent to the API, stored
 * on the Customer, and handed to PayPhone; it is never split apart below the
 * form (#103).
 *
 * Two tiers, mirroring ValidatePhone in phone.go exactly:
 *
 * - Ecuador (+593) is strict: a MOBILE, nine digits beginning with 9. An
 *   Ecuadorian landline is rejected because PayPhone's form wants a cardholder's
 *   mobile, and a single leading zero is dropped because that is the domestic
 *   trunk prefix an Ecuadorian reads off their own screen.
 * - Everywhere else: generic E.164, 4 to 15 digits in total including the
 *   dialling code, and nothing more. Permissive on purpose — a client rule
 *   stricter than the server's would refuse a sale the platform would have
 *   taken.
 */
export function normalizePhone(phone: string): string | null {
  const digits = scanPhone(phone);
  if (digits === null) return null;

  if (digits.startsWith(ECUADOR_DIGITS)) {
    let national = digits.slice(ECUADOR_DIGITS.length);
    if (national.startsWith("0")) national = national.slice(1);
    if (!/^9[0-9]{8}$/.test(national)) return null;
    return `${ECUADOR_DIALLING_CODE}${national}`;
  }

  if (digits.length < 4 || digits.length > 15) return null;
  return `+${digits}`;
}

/**
 * validatePhone returns the message to show under the field, or null when there
 * is nothing to complain about.
 *
 * A blank field is *not* an error: the phone is optional (#103), and a buyer who
 * skips it completes their purchase exactly as they do today. The caller simply
 * sends nothing. This is the one place the mirror is knowingly looser than the
 * server, whose ValidatePhone rejects the empty string — that is the correct
 * direction for the two to differ, and the server never sees a blank because
 * nothing is sent.
 *
 * The tier of a rejected number is read from what the buyer typed rather than
 * from what parsed, mirroring PhoneNumberMessage in phone.go, so someone
 * mistyping an Ecuadorian mobile is told about Ecuadorian mobiles.
 */
export function validatePhone(phone: string): string | null {
  if (phone.trim() === "") return null;
  if (normalizePhone(phone) !== null) return null;
  return phoneMessage(phone);
}

/**
 * phoneMessage picks the tier a rejected number was aiming at, from the digits
 * the buyer typed rather than from what parsed — the mirror of PhoneNumberMessage
 * in phone.go, factored out here for the same reason it is a named function
 * there: the tier choice is a rule, and a rule stated inline in one runtime and
 * named in the other is a rule that drifts.
 */
function phoneMessage(phone: string): string {
  return phone.replace(/[^0-9]/g, "").startsWith(ECUADOR_DIGITS)
    ? PHONE_ECUADOR_MESSAGE
    : PHONE_GENERIC_MESSAGE;
}

/**
 * composePhone assembles the two controls every phone form shows — the selector's
 * dialling code and the national number typed beside it — into the single string
 * the rule above is written against. Both the checkout dialog and "My info" go
 * through here, so the assembly exists once rather than once per form.
 *
 * An empty national number gives an empty string rather than a bare dialling
 * code, so "the buyer typed nothing" can never be mistaken for "+593" — a value
 * nobody entered, and one the optional-and-never-fabricated rule forbids (#103).
 *
 * A national number that ALREADY starts with a plus is taken whole and the
 * selector ignored. People paste. A buyer who drops "+12025550123" into the
 * field while the selector still reads Ecuador means the number they pasted, not
 * "+593+12025550123" — which would be refused here with a message about
 * Ecuadorian mobiles, while the server would have accepted the pasted number
 * verbatim. That is the mirror being STRICTER than the rule it mirrors, the one
 * direction this file may never differ in, and it would read to the buyer as the
 * platform refusing a perfectly good number.
 */
export function composePhone(diallingCode: string, nationalNumber: string): string {
  const national = nationalNumber.trim();
  if (national === "") return "";
  if (national.startsWith("+")) return national;
  return `${diallingCode}${national}`;
}

/**
 * splitPhone turns a stored canonical number back into the two halves a form
 * shows — the dialling code the selector sits on, and the national part in the
 * text field — by LONGEST-PREFIX match against the country table. Longest wins
 * so "+12425551234" resolves to the Bahamas' "+1242" rather than to "+1".
 *
 * It reads the dialling codes, never the named list: which halves a stored
 * number breaks into is a fact about the telephone network, and a number that
 * split differently for a Spanish reader than an English one would mean a buyer
 * switching language could submit a different number than the one they had.
 *
 * Nothing stored: Ecuador and an empty field, because Ecuador is the default
 * selection and the common case should need no interaction.
 *
 * No matching row: the whole value, plus included, is handed back as the
 * national part with an empty dialling code. This can only happen if the table
 * shrinks under a number already stored, and showing a buyer their own number
 * intact beats mangling it to fit a selector.
 */
export function splitPhone(phone: string | null | undefined): {
  diallingCode: string;
  nationalNumber: string;
} {
  if (!phone) return { diallingCode: ECUADOR_DIALLING_CODE, nationalNumber: "" };

  let match = "";
  for (const { diallingCode } of DIALLING_CODES) {
    if (phone.startsWith(diallingCode) && diallingCode.length > match.length) {
      match = diallingCode;
    }
  }
  if (match === "") return { diallingCode: "", nationalNumber: phone };
  return { diallingCode: match, nationalNumber: phone.slice(match.length) };
}
