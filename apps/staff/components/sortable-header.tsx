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
  // dense tightens the header's vertical padding to match a table whose body
  // rows are tight (the Sales list, #458). The Affiliate Links table, which
  // shares this header, keeps the roomier default.
  dense?: boolean;
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
  dense,
}: SortableHeaderProps<F>) {
  const active = sort === field;
  const padding = dense ? "py-1.5" : "py-2";
  return (
    <th
      className={
        numeric ? `${padding} pr-4 text-right font-medium` : `${padding} pr-4 font-medium`
      }
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
