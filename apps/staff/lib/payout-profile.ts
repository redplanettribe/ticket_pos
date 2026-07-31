// Pure Payout Profile helpers: where an Organization is paid (ADR 0026).
// Dependency-free — no DOM, no fetch — so they run directly under `node --test`
// (see payout-profile.test.ts) and can be reused by any surface that shows an
// account number.

/** The two account types an Ecuadorian bank offers, in the Spanish the receiving bank's form uses. */
export const ACCOUNT_TYPES = ["ahorros", "corriente"] as const;

/** The Tax ID Types a beneficiary may hold. Never `passport`: the party being paid holds an Ecuadorian bank account. */
export const PAYOUT_TAX_ID_TYPES = ["cedula", "ruc"] as const;

/**
 * Strips the separators an organizer brings with them when they copy an account
 * number off a bank statement — hyphens and whitespace — and leaves everything
 * else alone.
 *
 * A letter is deliberately kept rather than dropped: the server refuses it by
 * name, and silently deleting it here would turn a wrong number into a plausible
 * one, which is the worst outcome this field has. Leading zeros survive, which
 * is the whole reason the account number is text end to end.
 *
 * This mirrors NormalizeAccountNumber in backend/internal/sales/payoutprofile.go
 * so what an organizer sees while typing is what gets stored.
 */
export function normalizeAccountNumber(input: string): string {
  return input.replace(/[-\s]/g, "");
}

/**
 * Renders an account number the way it is safe to show beside other people's:
 * four dots and the last four digits, "····4821".
 *
 * The last four are the part a person uses to recognise their own account
 * without the number being readable over a shoulder or in a screenshot. A
 * number of four digits or fewer is masked entirely, because revealing "the
 * last four" of it would reveal all of it.
 */
export function maskAccountNumber(accountNumber: string): string {
  const normalized = normalizeAccountNumber(accountNumber);
  if (normalized.length <= 4) {
    return "····";
  }
  return `····${normalized.slice(-4)}`;
}
