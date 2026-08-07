import { Badge, OrgAvatar } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import type { FollowSuggestions, SessionOutcome, SuggestionReason } from "@/lib/customer-session";
import { followIntent } from "@/lib/follow-intent";
import { organizationFollowEndpoint, tagFollowEndpoint } from "@/lib/follows";
import { tagName, toTagTranslator } from "@/lib/tag-name";

import { FollowButton } from "./follow-button";
import { FollowRow, FollowRowList } from "./follow-row";

type FollowSuggestionsPanelProps = {
  /**
   * The suggestions read, OUTCOME AND ALL, rather than the data.
   *
   * The panel decides what a failure means, because the answer is "nothing" and
   * a page that had to remember that would eventually forget. See below.
   */
  suggestions: SessionOutcome<FollowSuggestions>;
};

/**
 * Suggested Follows: Tags and Organizations the Customer does not Follow, beneath
 * the ones they do (#231 and #232, parent #229, ADR 0031).
 *
 * EACH SUGGESTION SAYS WHICH OF THE CUSTOMER'S INTERESTS PRODUCED IT, when one
 * did — the Tag it co-occurs with on real Events, worded here rather than sent
 * as a sentence. A suggestion ranked on Activity alone carries no reason and
 * gets no line, which is honest rather than incomplete: it is offered because
 * things are happening under it, and "because it is busy" is not a reason worth
 * a reader's line.
 *
 * BELOW THE CUSTOMER'S OWN LIST, in the same place whether or not they Follow
 * anything. Above it would put things the Customer did not choose ahead of the
 * things they did, on the one page that is theirs, and would separate the Follow
 * Digest switch from the list whose meaning it changes — an adjacency the page
 * arranges deliberately.
 *
 * TWO GROUPS, NOT ONE LIST, and the difference is visible before a word is read:
 * Tags are a compact chip row, Organizations are rows in the Following list's own
 * shape (see follow-row.tsx, which both draw from). The API ranks the two kinds
 * over different populations, so interleaving them would have invented an order
 * across them that means nothing.
 *
 * A FAILED READ IS NO PANEL, SILENTLY. Nobody arrives at this page for
 * suggestions — they come to see or to manage what they Follow — so an error
 * banner about a feature they never asked for is worse than its absence, and it
 * would sit directly beneath the listing's own error handling saying something
 * about a different failure. The Follows listing keeps that handling unchanged.
 *
 * The same silence covers an empty result, which is not a failure at all: a
 * Customer who already Follows everything with upcoming Events behind it is this
 * feature working. There is no empty state, because an empty box offering nothing
 * is worse than nothing.
 *
 * A server component. The only interactive part is the Follow control, which is
 * already a client island, and which is the SAME control against the SAME
 * endpoints the Event page uses — there is no follow written specially for this
 * panel. Its own refresh is what moves an accepted suggestion up into the list
 * above and out of this one.
 */
export async function FollowSuggestionsPanel({ suggestions }: FollowSuggestionsPanelProps) {
  if (suggestions.status !== "ok") return null;

  const { tags, organizations } = suggestions.data;
  if (tags.length === 0 && organizations.length === 0) return null;

  const t = await getTranslations("following");
  // The `tags` namespace, for wording Preset Tags in this page's Locale. A
  // Custom Tag is rendered as its Organization coined it; `tagName` decides which
  // is which here exactly as it does in the list above (ADR 0027).
  const tTags = toTagTranslator(await getTranslations("tags"));

  /**
   * Why a suggestion is here, as a sentence in THIS PAGE'S LANGUAGE.
   *
   * The API sends the producing Tag and never a sentence, so the wording is
   * composed here from this app's own catalogues — exactly as every other Tag on
   * the platform is worded, and for the same reason: an API with no idea which
   * Locale a page is in cannot write for it (ADR 0027).
   *
   * A Custom Tag has no Spanish name by constraint (ADR 0031, migration 050), so
   * this line can put an English word inside a Spanish sentence. That is an
   * accepted consequence of Custom Tags being suggestible at all rather than a
   * gap in the catalogue, and the copy quotes the name — «Techno» — so a reader
   * meets it as somebody's own word rather than as a sentence that failed to
   * translate.
   *
   * EVERY SUGGESTION GETS A LINE, and the absent reason is a kind of reason
   * rather than the lack of one. A null arrives on every subject the Activity
   * ranking supplied — the tier that fills the panel behind Co-occurrence, and
   * the whole of the panel for a Customer who Follows nothing yet — and what
   * put those there is Activity: there is a lot on under them. That is as true
   * and as sayable as "goes with Jazz", so it is said. Rendering silence
   * instead, which this did, left the reader of a mixed panel to guess why half
   * of it was there, and left a Customer with no Follows at all — the one least
   * equipped to guess — with a row of chips and no explanation of any kind.
   *
   * The two tiers are told apart by this line alone and never by a heading. The
   * boundary between them moves with the Customer and with the catalogue, so a
   * heading would sometimes label a single chip, and it would have to re-order
   * the entries — and the order is the ranking.
   *
   * NOTHING CAN SILENCE THIS LINE (#234): the Co-occurrence reason carries the
   * producing Tag whole, so it is worded from what arrived and from nothing
   * else. It used to be looked up in the Follows listing by canonical key, which
   * held while every producer was a Tag the Customer Follows DIRECTLY, and
   * stopped holding the moment a producer could be a DERIVED Tag — one inferred
   * from a followed Organization's upcoming Events, which ADR 0031 keeps out of
   * that listing by design (#233). A Customer who Follows only Organizations
   * then met a full panel with every reason line missing, which is the reader
   * that ticket exists for.
   */
  function reasonLine(reason: SuggestionReason | null): string {
    // Activity, said plainly: supply and never audience. Nothing here is
    // "popular" and no count of Followers exists to make it so (CONTEXT.md), and
    // it deliberately avoids "New to You", which reads naturally in this slot
    // and already means something else in this very feature — an Event never yet
    // carried by a Follow Digest.
    if (!reason) return t("suggestionActivityReason");
    return t("suggestionReason", { tag: tagName(reason.tag, tTags) });
  }

  return (
    <section className="space-y-4">
      <div>
        <h2 className="font-medium">{t("suggestionsTitle")}</h2>
        {/* What the panel IS, and no more than that. It used to say these have
            events coming up, which was a claim on behalf of every entry and was
            right while Activity was the only ranking; it now describes one tier
            of two, so it moved down to the entries that own it. A heading that
            asserts something true of half the list below it is worse than one
            that asserts nothing. */}
        <p className="text-muted-foreground text-sm">{t("suggestionsDescription")}</p>
      </div>

      {tags.length > 0 ? (
        // Aligned to the TOP rather than the middle, because each chip may carry
        // a reason beneath it and one that does must not shove its neighbours
        // down a half-line.
        <div className="flex flex-wrap items-start gap-x-2 gap-y-3">
          {tags.map(({ tag, reason }) => {
            const name = tagName(tag, tTags);
            const why = reasonLine(reason);
            return (
              // The chip and its reason as ONE column, so the sentence stays
              // attached to the Tag it is about. The alternative — grouping the
              // chips under one heading per reason — would have had to re-order
              // them, and the order is the ranking.
              <span key={tag.canonical_key} className="inline-flex flex-col gap-1">
                <span
                  // Deliberately the SAME pill as the Event page's Tag row and the
                  // explorer's filters, down to the radius and the padding. A Tag is
                  // one thing wherever it is drawn.
                  className="border-input bg-background inline-flex items-center self-start rounded-full border"
                >
                  <span className="text-foreground py-1 pr-2 pl-3 text-sm">{name}</span>
                  {!tag.curated ? (
                    // The Following list's existing badge, in its existing words. A
                    // Custom Tag carries no Spanish name by constraint (ADR 0027,
                    // migration 050), so a Spanish reader meets an English word —
                    // and is owed the marker here more than in the list above,
                    // because the platform rather than the reader put it there.
                    <Badge variant="outline" className="mr-1 text-[10px]">
                      {t("customTag")}
                    </Badge>
                  ) : null}
                  <FollowButton
                    endpoint={tagFollowEndpoint(tag.canonical_key)}
                    // A suggestion is by definition something the Customer does not
                    // Follow: the API excludes what they do. Nothing is re-derived
                    // here from the Follows listing.
                    following={false}
                    subjectName={name}
                    // The canonical key, never the rendered name — the intent has to
                    // name the same pool row whatever language the reader is in.
                    intent={followIntent("tag", tag.canonical_key)}
                    // This page is behind the session, so the control is never the
                    // signed-out kind.
                    signedIn
                    compact
                    className="rounded-full"
                  />
                </span>
                <span className="text-muted-foreground px-1 text-xs">{why}</span>
              </span>
            );
          })}
        </div>
      ) : null}

      {organizations.length > 0 ? (
        <FollowRowList>
          {organizations.map(({ organization, reason }) => {
            const why = reasonLine(reason);
            return (
              <FollowRow
                key={organization.slug}
                media={<OrgAvatar logoUrl={organization.logo_url} name={organization.name} />}
                href={`/${organization.slug}`}
                name={organization.name}
                // The reason rides on the line that already says what kind of
                // thing this is, rather than adding a third line to a row shape
                // the Following list above shares. `kind` takes a node for
                // exactly this: a suggested Organization has something more to
                // say about itself than a followed one does, and only here.
                kind={`${t("organizationKind")} · ${why}`}
                control={
                  <FollowButton
                    endpoint={organizationFollowEndpoint(organization.slug)}
                    following={false}
                    subjectName={organization.name}
                    intent={followIntent("organization", organization.slug)}
                    signedIn
                    compact
                  />
                }
              />
            );
          })}
        </FollowRowList>
      ) : null}
    </section>
  );
}
