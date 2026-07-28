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

/**
 * A row of the country selector: the name a buyer looks for and the dialling
 * code that gets prepended to what they type.
 */
export type Country = {
  name: string;
  diallingCode: string;
};

/** Ecuador's dialling code — the default selection, and the one strict tier. */
export const ECUADOR_DIALLING_CODE = "+593";

/**
 * The country table.
 *
 * It carries NO validation duty whatsoever: it supplies the prefix for display
 * and for assembling the canonical number, and that is all. Validation is the
 * two tiers in `normalizePhone` below, which key off the dialling code in the
 * number itself, not off this list. Adding or removing a row changes what the
 * selector offers and nothing about what is accepted.
 *
 * Ecuador is first because it is the default selection and the overwhelming
 * majority of buyers — the common case should need no interaction at all. The
 * rest are alphabetical, which is the only order a person scanning a long list
 * can predict.
 *
 * Accepted cosmetic imperfection (#103): `+1` covers the United States, Canada
 * and some twenty other countries, and `+7` covers Russia and Kazakhstan. The
 * longest-prefix match in `splitPhone` therefore resolves a stored `+1` number
 * to whichever `+1` row appears first here — Canada, alphabetically — regardless
 * of where the buyer actually is. This is deliberately not solved: the value
 * sent to PayPhone is unaffected, split dialling-code columns would not have
 * recorded the distinction either, and the alternative is a second table of
 * area-code ranges maintained forever. The Caribbean NANP territories are listed
 * with their full three-digit `+1XXX` codes, so those at least resolve exactly.
 */
export const COUNTRIES: readonly Country[] = [
  { name: "Ecuador", diallingCode: ECUADOR_DIALLING_CODE },
  { name: "Afghanistan", diallingCode: "+93" },
  { name: "Albania", diallingCode: "+355" },
  { name: "Algeria", diallingCode: "+213" },
  { name: "Andorra", diallingCode: "+376" },
  { name: "Angola", diallingCode: "+244" },
  { name: "Antigua and Barbuda", diallingCode: "+1268" },
  { name: "Argentina", diallingCode: "+54" },
  { name: "Armenia", diallingCode: "+374" },
  { name: "Aruba", diallingCode: "+297" },
  { name: "Australia", diallingCode: "+61" },
  { name: "Austria", diallingCode: "+43" },
  { name: "Azerbaijan", diallingCode: "+994" },
  { name: "Bahamas", diallingCode: "+1242" },
  { name: "Bahrain", diallingCode: "+973" },
  { name: "Bangladesh", diallingCode: "+880" },
  { name: "Barbados", diallingCode: "+1246" },
  { name: "Belarus", diallingCode: "+375" },
  { name: "Belgium", diallingCode: "+32" },
  { name: "Belize", diallingCode: "+501" },
  { name: "Benin", diallingCode: "+229" },
  { name: "Bermuda", diallingCode: "+1441" },
  { name: "Bhutan", diallingCode: "+975" },
  { name: "Bolivia", diallingCode: "+591" },
  { name: "Bosnia and Herzegovina", diallingCode: "+387" },
  { name: "Botswana", diallingCode: "+267" },
  { name: "Brazil", diallingCode: "+55" },
  { name: "Brunei", diallingCode: "+673" },
  { name: "Bulgaria", diallingCode: "+359" },
  { name: "Burkina Faso", diallingCode: "+226" },
  { name: "Burundi", diallingCode: "+257" },
  { name: "Cambodia", diallingCode: "+855" },
  { name: "Cameroon", diallingCode: "+237" },
  { name: "Canada", diallingCode: "+1" },
  { name: "Cape Verde", diallingCode: "+238" },
  { name: "Cayman Islands", diallingCode: "+1345" },
  { name: "Central African Republic", diallingCode: "+236" },
  { name: "Chad", diallingCode: "+235" },
  { name: "Chile", diallingCode: "+56" },
  { name: "China", diallingCode: "+86" },
  { name: "Colombia", diallingCode: "+57" },
  { name: "Comoros", diallingCode: "+269" },
  { name: "Congo (Democratic Republic)", diallingCode: "+243" },
  { name: "Congo (Republic)", diallingCode: "+242" },
  { name: "Costa Rica", diallingCode: "+506" },
  { name: "Côte d'Ivoire", diallingCode: "+225" },
  { name: "Croatia", diallingCode: "+385" },
  { name: "Cuba", diallingCode: "+53" },
  { name: "Curaçao", diallingCode: "+599" },
  { name: "Cyprus", diallingCode: "+357" },
  { name: "Czechia", diallingCode: "+420" },
  { name: "Denmark", diallingCode: "+45" },
  { name: "Djibouti", diallingCode: "+253" },
  { name: "Dominica", diallingCode: "+1767" },
  { name: "Dominican Republic", diallingCode: "+1809" },
  { name: "Egypt", diallingCode: "+20" },
  { name: "El Salvador", diallingCode: "+503" },
  { name: "Equatorial Guinea", diallingCode: "+240" },
  { name: "Eritrea", diallingCode: "+291" },
  { name: "Estonia", diallingCode: "+372" },
  { name: "Eswatini", diallingCode: "+268" },
  { name: "Ethiopia", diallingCode: "+251" },
  { name: "Fiji", diallingCode: "+679" },
  { name: "Finland", diallingCode: "+358" },
  { name: "France", diallingCode: "+33" },
  { name: "French Guiana", diallingCode: "+594" },
  { name: "French Polynesia", diallingCode: "+689" },
  { name: "Gabon", diallingCode: "+241" },
  { name: "Gambia", diallingCode: "+220" },
  { name: "Georgia", diallingCode: "+995" },
  { name: "Germany", diallingCode: "+49" },
  { name: "Ghana", diallingCode: "+233" },
  { name: "Gibraltar", diallingCode: "+350" },
  { name: "Greece", diallingCode: "+30" },
  { name: "Greenland", diallingCode: "+299" },
  { name: "Grenada", diallingCode: "+1473" },
  { name: "Guadeloupe", diallingCode: "+590" },
  { name: "Guam", diallingCode: "+1671" },
  { name: "Guatemala", diallingCode: "+502" },
  { name: "Guinea", diallingCode: "+224" },
  { name: "Guinea-Bissau", diallingCode: "+245" },
  { name: "Guyana", diallingCode: "+592" },
  { name: "Haiti", diallingCode: "+509" },
  { name: "Honduras", diallingCode: "+504" },
  { name: "Hong Kong", diallingCode: "+852" },
  { name: "Hungary", diallingCode: "+36" },
  { name: "Iceland", diallingCode: "+354" },
  { name: "India", diallingCode: "+91" },
  { name: "Indonesia", diallingCode: "+62" },
  { name: "Iran", diallingCode: "+98" },
  { name: "Iraq", diallingCode: "+964" },
  { name: "Ireland", diallingCode: "+353" },
  { name: "Israel", diallingCode: "+972" },
  { name: "Italy", diallingCode: "+39" },
  { name: "Jamaica", diallingCode: "+1876" },
  { name: "Japan", diallingCode: "+81" },
  { name: "Jordan", diallingCode: "+962" },
  { name: "Kazakhstan", diallingCode: "+7" },
  { name: "Kenya", diallingCode: "+254" },
  { name: "Kiribati", diallingCode: "+686" },
  { name: "Kosovo", diallingCode: "+383" },
  { name: "Kuwait", diallingCode: "+965" },
  { name: "Kyrgyzstan", diallingCode: "+996" },
  { name: "Laos", diallingCode: "+856" },
  { name: "Latvia", diallingCode: "+371" },
  { name: "Lebanon", diallingCode: "+961" },
  { name: "Lesotho", diallingCode: "+266" },
  { name: "Liberia", diallingCode: "+231" },
  { name: "Libya", diallingCode: "+218" },
  { name: "Liechtenstein", diallingCode: "+423" },
  { name: "Lithuania", diallingCode: "+370" },
  { name: "Luxembourg", diallingCode: "+352" },
  { name: "Macao", diallingCode: "+853" },
  { name: "Madagascar", diallingCode: "+261" },
  { name: "Malawi", diallingCode: "+265" },
  { name: "Malaysia", diallingCode: "+60" },
  { name: "Maldives", diallingCode: "+960" },
  { name: "Mali", diallingCode: "+223" },
  { name: "Malta", diallingCode: "+356" },
  { name: "Marshall Islands", diallingCode: "+692" },
  { name: "Martinique", diallingCode: "+596" },
  { name: "Mauritania", diallingCode: "+222" },
  { name: "Mauritius", diallingCode: "+230" },
  { name: "Mexico", diallingCode: "+52" },
  { name: "Micronesia", diallingCode: "+691" },
  { name: "Moldova", diallingCode: "+373" },
  { name: "Monaco", diallingCode: "+377" },
  { name: "Mongolia", diallingCode: "+976" },
  { name: "Montenegro", diallingCode: "+382" },
  { name: "Morocco", diallingCode: "+212" },
  { name: "Mozambique", diallingCode: "+258" },
  { name: "Myanmar", diallingCode: "+95" },
  { name: "Namibia", diallingCode: "+264" },
  { name: "Nauru", diallingCode: "+674" },
  { name: "Nepal", diallingCode: "+977" },
  { name: "Netherlands", diallingCode: "+31" },
  { name: "New Caledonia", diallingCode: "+687" },
  { name: "New Zealand", diallingCode: "+64" },
  { name: "Nicaragua", diallingCode: "+505" },
  { name: "Niger", diallingCode: "+227" },
  { name: "Nigeria", diallingCode: "+234" },
  { name: "North Korea", diallingCode: "+850" },
  { name: "North Macedonia", diallingCode: "+389" },
  { name: "Norway", diallingCode: "+47" },
  { name: "Oman", diallingCode: "+968" },
  { name: "Pakistan", diallingCode: "+92" },
  { name: "Palau", diallingCode: "+680" },
  { name: "Palestine", diallingCode: "+970" },
  { name: "Panama", diallingCode: "+507" },
  { name: "Papua New Guinea", diallingCode: "+675" },
  { name: "Paraguay", diallingCode: "+595" },
  { name: "Peru", diallingCode: "+51" },
  { name: "Philippines", diallingCode: "+63" },
  { name: "Poland", diallingCode: "+48" },
  { name: "Portugal", diallingCode: "+351" },
  { name: "Puerto Rico", diallingCode: "+1787" },
  { name: "Qatar", diallingCode: "+974" },
  { name: "Réunion", diallingCode: "+262" },
  { name: "Romania", diallingCode: "+40" },
  { name: "Russia", diallingCode: "+7" },
  { name: "Rwanda", diallingCode: "+250" },
  { name: "Saint Kitts and Nevis", diallingCode: "+1869" },
  { name: "Saint Lucia", diallingCode: "+1758" },
  { name: "Saint Vincent and the Grenadines", diallingCode: "+1784" },
  { name: "Samoa", diallingCode: "+685" },
  { name: "San Marino", diallingCode: "+378" },
  { name: "São Tomé and Príncipe", diallingCode: "+239" },
  { name: "Saudi Arabia", diallingCode: "+966" },
  { name: "Senegal", diallingCode: "+221" },
  { name: "Serbia", diallingCode: "+381" },
  { name: "Seychelles", diallingCode: "+248" },
  { name: "Sierra Leone", diallingCode: "+232" },
  { name: "Singapore", diallingCode: "+65" },
  { name: "Sint Maarten", diallingCode: "+1721" },
  { name: "Slovakia", diallingCode: "+421" },
  { name: "Slovenia", diallingCode: "+386" },
  { name: "Solomon Islands", diallingCode: "+677" },
  { name: "Somalia", diallingCode: "+252" },
  { name: "South Africa", diallingCode: "+27" },
  { name: "South Korea", diallingCode: "+82" },
  { name: "South Sudan", diallingCode: "+211" },
  { name: "Spain", diallingCode: "+34" },
  { name: "Sri Lanka", diallingCode: "+94" },
  { name: "Sudan", diallingCode: "+249" },
  { name: "Suriname", diallingCode: "+597" },
  { name: "Sweden", diallingCode: "+46" },
  { name: "Switzerland", diallingCode: "+41" },
  { name: "Syria", diallingCode: "+963" },
  { name: "Taiwan", diallingCode: "+886" },
  { name: "Tajikistan", diallingCode: "+992" },
  { name: "Tanzania", diallingCode: "+255" },
  { name: "Thailand", diallingCode: "+66" },
  { name: "Timor-Leste", diallingCode: "+670" },
  { name: "Togo", diallingCode: "+228" },
  { name: "Tonga", diallingCode: "+676" },
  { name: "Trinidad and Tobago", diallingCode: "+1868" },
  { name: "Tunisia", diallingCode: "+216" },
  { name: "Turkey", diallingCode: "+90" },
  { name: "Turkmenistan", diallingCode: "+993" },
  { name: "Turks and Caicos Islands", diallingCode: "+1649" },
  { name: "Tuvalu", diallingCode: "+688" },
  { name: "Uganda", diallingCode: "+256" },
  { name: "Ukraine", diallingCode: "+380" },
  { name: "United Arab Emirates", diallingCode: "+971" },
  { name: "United Kingdom", diallingCode: "+44" },
  { name: "United States", diallingCode: "+1" },
  { name: "Uruguay", diallingCode: "+598" },
  { name: "Uzbekistan", diallingCode: "+998" },
  { name: "Vanuatu", diallingCode: "+678" },
  { name: "Vatican City", diallingCode: "+379" },
  { name: "Venezuela", diallingCode: "+58" },
  { name: "Vietnam", diallingCode: "+84" },
  { name: "Virgin Islands (British)", diallingCode: "+1284" },
  { name: "Virgin Islands (U.S.)", diallingCode: "+1340" },
  { name: "Yemen", diallingCode: "+967" },
  { name: "Zambia", diallingCode: "+260" },
  { name: "Zimbabwe", diallingCode: "+263" },
];

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
  for (const country of COUNTRIES) {
    if (phone.startsWith(country.diallingCode) && country.diallingCode.length > match.length) {
      match = country.diallingCode;
    }
  }
  if (match === "") return { diallingCode: "", nationalNumber: phone };
  return { diallingCode: match, nationalNumber: phone.slice(match.length) };
}
