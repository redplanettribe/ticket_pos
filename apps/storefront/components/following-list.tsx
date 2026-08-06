import { Badge, OrgAvatar } from "@ticket-pos/ui";
import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
import type { Follow } from "@/lib/customer-session";
import { followIntent } from "@/lib/follow-intent";
import { organizationFollowEndpoint, tagFollowEndpoint } from "@/lib/follows";
import { tagName, toTagTranslator } from "@/lib/tag-name";

import { EmptyState } from "./event-grid";
import { FollowButton } from "./follow-button";

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
    <ul className="divide-y rounded-lg border">
      {follows.map((follow) => {
        if (follow.type === "organization") {
          const { organization } = follow;
          return (
            <li
              key={`organization:${organization.slug}`}
              className="flex items-center justify-between gap-4 p-4"
            >
              <div className="flex min-w-0 items-center gap-3">
                <OrgAvatar logoUrl={organization.logo_url} name={organization.name} />
                <div className="min-w-0">
                  {/* The row is a way back to what is followed, not only a way
                      out of following it. */}
                  <Link
                    href={`/${organization.slug}`}
                    className="block truncate font-medium hover:underline"
                  >
                    {organization.name}
                  </Link>
                  <p className="text-muted-foreground text-xs">{t("organizationKind")}</p>
                </div>
              </div>
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
            </li>
          );
        }

        const { tag } = follow;
        const name = tagName(tag, tTags);
        return (
          <li
            key={`tag:${tag.canonical_key}`}
            className="flex items-center justify-between gap-4 p-4"
          >
            <div className="flex min-w-0 items-center gap-3">
              {/* A Tag's counterpart to the Organization's logo, so the two
                  kinds line up down the left edge instead of one row starting
                  further in than the other. A glyph rather than an icon: this
                  app does not depend on the icon set, and "#" is what a tag
                  reads as anyway. */}
              <span
                aria-hidden
                className="bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center rounded-md text-sm font-medium"
              >
                #
              </span>
              <div className="min-w-0">
                {/* A Tag has no page of its own, so it links to the explorer
                    filtered by it — the nearest thing to "what I followed this
                    for", and the same address the explorer's own chips produce. */}
                <Link
                  href={`/?tags=${encodeURIComponent(tag.canonical_key)}`}
                  className="block truncate font-medium hover:underline"
                >
                  {name}
                </Link>
                <p className="text-muted-foreground text-xs">
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
                </p>
              </div>
            </div>
            <FollowButton
              endpoint={tagFollowEndpoint(tag.canonical_key)}
              following
              subjectName={name}
              intent={followIntent("tag", tag.canonical_key)}
              signedIn
              compact
            />
          </li>
        );
      })}
    </ul>
  );
}
