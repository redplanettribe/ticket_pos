"use client";

export type SortDirection = "asc" | "desc";

export type SortableHeaderProps<F extends string> = {
  label: string;
  field: F;
  sort: F;
  dir: SortDirection;
  onSort: (field: F) => void;
  // numeric right-aligns the header over a column of figures.
  numeric?: boolean;
  // title, when given, names the active sort for a pointer resting on the
  // arrow; an inactive column carries no title.
  title?: { ascending: string; descending: string };
};

// SortableHeader is a column header that toggles a table's sort. The active
// column shows a direction arrow; clicking flips it, clicking another column
// switches to it. aria-sort exposes the state to assistive tech.
//
// It is generic over the table's sort field union so each table keeps its own
// allowlist: the Sales list and the Affiliate Links table share the header, not
// the columns.
//
// `label` arrives translated rather than as a key: the header is one of several
// things this component is handed, and the surface above owns its own words.
export function SortableHeader<F extends string>({
  label,
  field,
  sort,
  dir,
  onSort,
  numeric,
  title,
}: SortableHeaderProps<F>) {
  const active = sort === field;
  return (
    <th
      className={numeric ? "py-2 pr-4 text-right font-medium" : "py-2 pr-4 font-medium"}
      aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(field)}
        title={active && title ? (dir === "asc" ? title.ascending : title.descending) : undefined}
        className="inline-flex items-center gap-1 uppercase tracking-wide hover:text-foreground"
      >
        {label}
        <span aria-hidden className={active ? "text-foreground" : "text-muted-foreground/40"}>
          {active ? (dir === "asc" ? "▲" : "▼") : "↕"}
        </span>
      </button>
    </th>
  );
}
