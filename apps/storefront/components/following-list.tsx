import { Badge, OrgAvatar } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import type { Follow } from "@/lib/customer-session";
import { followIntent } from "@/lib/follow-intent";
import { organizationFollowEndpoint, tagFollowEndpoint } from "@/lib/follows";
import { tagName, toTagTranslator } from "@/lib/tag-name";

import { EmptyState } from "./event-grid";
import { FollowButton } from "./follow-button";
import { FollowRow, FollowRowList, TagRowGlyph } from "./follow-row";

type FollowingListProps = {
  /** The Follows as the API returned them: both kinds, already in one order. */
  follows: Follow[];
};

/**
 * The Customer's Follows, of both kinds, as one list they can manage (#218).
 *
 * ONE LIST, NOT TWO SECTIONS. The API hands back both kinds interleaved by when
 * each Follow was made, and this renders exactly that order — which is the
 * useful one, because a person arriving here has usually just followed something
 * and is looking for it at the top. Grouping by kind would have buried it under
 * whichever group it fell into, and would have made this page the second place
 * in the system deciding what order Follows come in.
 *
 * A server component: the only interactive part is the Follow control itself,
 * which is already a client island, so nothing here needs to ship.
 *
 * Each row offers Unfollow through the same control the public surfaces use,
 * pointing at the same endpoint. There is no unfollow written specially for this
 * page — a Follow removed here and a Follow removed from an Event page are the
 * same request, and the control's own `router.refresh()` is what takes the row
 * away once the server agrees.
 */
export async function FollowingList({ follows }: FollowingListProps) {
  const t = await getTranslations("following");

  if (follows.length === 0) {
    // The empty state says what a Follow is FOR and where to make one, because
    // an empty list is the only screen in this feature a person can reach
    // without ever having seen the control that fills it.
    return <EmptyState title={t("emptyTitle")} description={t("emptyDescription")} />;
  }

  // The `tags` namespace, for wording Preset Tags in this page's Locale. Custom
  // Tags are rendered as their Organization coined them; `tagName` decides which
  // is which, here as everywhere else (ADR 0027).
  const tTags = toTagTranslator(await getTranslations("tags"));

  return (
    <FollowRowList>
      {follows.map((follow) => {
        if (follow.type === "organization") {
          const { organization } = follow;
          return (
            <FollowRow
              key={`organization:${organization.slug}`}
              media={<OrgAvatar logoUrl={organization.logo_url} name={organization.name} />}
              // The row is a way back to what is followed, not only a way out of
              // following it.
              href={`/${organization.slug}`}
              name={organization.name}
              kind={t("organizationKind")}
              control={
                <FollowButton
                  endpoint={organizationFollowEndpoint(organization.slug)}
                  following
                  subjectName={organization.name}
                  intent={followIntent("organization", organization.slug)}
                  // The Customer Area is behind the session, so this control is
                  // never the signed-out kind. The intent is passed anyway rather
                  // than faked, so the prop means one thing everywhere.
                  signedIn
                  compact
                />
              }
            />
          );
        }

        const { tag } = follow;
        const name = tagName(tag, tTags);
        return (
          <FollowRow
            key={`tag:${tag.canonical_key}`}
            media={<TagRowGlyph />}
            // A Tag has no page of its own, so it links to the explorer filtered
            // by it — the nearest thing to "what I followed this for", and the
            // same address the explorer's own chips produce.
            href={`/?tags=${encodeURIComponent(tag.canonical_key)}`}
            name={name}
            kind={
              <>
                {t("tagKind")}
                {!tag.curated ? (
                  <>
                    {" "}
                    {/* Which kind of Tag it is, said plainly: a Custom Tag is
                        one Organization's own word and is not worded in the
                        reader's language, so a reader seeing an English word
                        in a Spanish list is owed the reason. */}
                    <Badge variant="outline" className="ml-1 align-middle text-[10px]">
                      {t("customTag")}
                    </Badge>
                  </>
                ) : null}
              </>
            }
            control={
              <FollowButton
                endpoint={tagFollowEndpoint(tag.canonical_key)}
                following
                subjectName={name}
                intent={followIntent("tag", tag.canonical_key)}
                signedIn
                compact
              />
            }
          />
        );
      })}
    </FollowRowList>
  );
}
