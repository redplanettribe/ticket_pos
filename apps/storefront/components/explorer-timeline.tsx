"use client";

import { useTranslations } from "next-intl";

import { useFormatLocale } from "@/i18n/format-locale";
import type { PublicEventCard } from "@/lib/api";
import { ECUADOR_TIME_ZONE } from "@/lib/format";
import { buildTimeline, localDateKey } from "@/lib/timeline";

import { TimelineEventCard } from "./timeline-event-card";

type ExplorerTimelineProps = {
  /** The accumulated feed, in the server's start-instant order. */
  events: PublicEventCard[];
  /**
   * The render's clock, decided by the page. Grouping must not read the wall
   * clock itself: the same `now` has to govern the server render and every
   * client re-render after it, or an Event could hop between Ongoing and its
   * Day Bucket mid-"Load more".
   */
  now: Date;
};

/**
 * The Timeline (CONTEXT.md): the explorer's one view of the feed — the Ongoing
 * group first, then a section per Day Bucket. Grouping is derived from the flat
 * feed on every render; nothing here holds state.
 */
export function ExplorerTimeline({ events, now }: ExplorerTimelineProps) {
  const t = useTranslations("explorer.timeline");
  const locale = useFormatLocale();
  const timeline = buildTimeline(events, now);

  // The year is left off headers within the platform's current year — "Aug 29"
  // — and said only where it differs, the same judgement "Today" is made by:
  // the platform wall clock, not the viewer's (CONTEXT.md: Day Bucket).
  const platformYear = localDateKey(now, ECUADOR_TIME_ZONE).slice(0, 4);

  return (
    // The spine: on sm+ a continuous vertical line runs the whole Timeline at
    // the boundary between the date rail and the cards, and each group pins a
    // dot to it. On mobile the rail collapses into a plain header above the
    // cards and the line goes with it.
    <div className="relative">
      <div
        aria-hidden
        className="absolute inset-y-1 left-[7.75rem] hidden w-px bg-border sm:block"
      />
      <div className="space-y-10">
        {timeline.ongoing.length > 0 ? (
          <TimelineSection heading={t("ongoing")} events={timeline.ongoing} showStartDate />
        ) : null}
        {timeline.days.map((day) => {
        // A Day Bucket's key is a plain date; pinning it to noon UTC and
        // formatting in UTC reads that date back verbatim, no zone arithmetic.
        const date = new Date(`${day.key}T12:00:00Z`);
        const dateLabel = new Intl.DateTimeFormat(locale, {
          month: "short",
          day: "numeric",
          ...(day.key.slice(0, 4) === platformYear ? {} : { year: "numeric" }),
          timeZone: "UTC",
        }).format(date);
        const weekday = new Intl.DateTimeFormat(locale, {
          weekday: "long",
          timeZone: "UTC",
        }).format(date);
          return (
            <TimelineSection
              key={day.key}
              heading={day.relative ? t(day.relative) : dateLabel}
              subheading={weekday}
              events={day.events}
            />
          );
        })}
      </div>
    </div>
  );
}

type TimelineSectionProps = {
  heading: string;
  subheading?: string;
  events: PublicEventCard[];
  /** Passed through to the cards; true only for the Ongoing group. */
  showStartDate?: boolean;
};

function TimelineSection({ heading, subheading, events, showStartDate }: TimelineSectionProps) {
  return (
    <section className="relative sm:grid sm:grid-cols-[7rem_1fr] sm:gap-8">
      {/* The group's dot, centred on the spine the parent draws. */}
      <span
        aria-hidden
        className="absolute left-[7.75rem] top-1.5 hidden h-2 w-2 -translate-x-1/2 rounded-full bg-muted-foreground/60 sm:block"
      />
      <h2 className="mb-3 flex items-baseline gap-2 sm:sticky sm:top-24 sm:mb-0 sm:block sm:self-start">
        <span className="block font-semibold tracking-tight">{heading}</span>
        {subheading ? (
          <span className="block text-sm font-normal text-muted-foreground">{subheading}</span>
        ) : null}
      </h2>
      <div className="space-y-4">
        {events.map((event) => (
          <TimelineEventCard
            key={`${event.organization.slug}/${event.slug}`}
            event={event}
            showStartDate={showStartDate}
          />
        ))}
      </div>
    </section>
  );
}
