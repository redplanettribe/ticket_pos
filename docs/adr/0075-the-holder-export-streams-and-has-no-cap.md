# The Holder Export streams end to end and has no cap

Supersedes in part [ADR 0065](./0065-the-holder-list-filters-and-downloads-itself-and-an-event-owner-may-read-it.md): its 2,000-Ticket cap, and the refusal over it.
Everything else ADR 0065 decided about the Holder Export stands - its contents, its Info sheet, its access rule, its disclosure rule and its audit line's fields.
Leaves [ADR 0032](./0032-sales-export-states-net-proceeds-never-itemises-the-platform-fee.md)'s Sales Export file as it stands: every value is unchanged, one cell's encoding aside (a date Answer before 1900, below), and its response gains only a `Cache-Control: no-store` header.

## Context

ADR 0065 capped the Holder Export at 2,000 Tickets, and the benchmark behind that number (#530) found what binds it: memory, not time.
excelize holds the whole workbook in process memory at roughly 600 B per cell.
50,000 rows across 219 columns built in 22 seconds against a 300s request timeout, and took 6.9 GiB against an `api_memory` of 512Mi.
Even with no Ticket Questions at all, 50,000 rows needed 556 MiB.

2,000 is what the widest plausible Event can afford, so it is far below what an organizer of any real size needs from "who is coming".
ADR 0065 already said the fix is not a bigger constant but a change to what is bounded.

excelize's own StreamWriter is not that change on this platform.
It spills each sheet's XML to a temp file past 16 MiB, and Cloud Run's filesystem is in memory, so the spill still counts against the instance's limit.
Memory would grow with the file at the size of the uncompressed sheet XML - about 40 B per cell, so a wide 50,000-row roster still lands around 440 MiB.
That is a higher ceiling, not the absence of one.

## Decision

**The Holder Export has no row cap.**
It streams end to end: the rows come off a Postgres cursor, are written as sheet XML, compressed, and sent into the HTTP response as they are produced.
Nothing holds the whole roster or the whole file in memory.
The only ceiling left is Cloud Run's 300s request timeout, which the #530 measurement puts at roughly half a million Tickets and beyond.

**The workbook is written by a minimal xlsx writer of our own, not excelize.**
An xlsx file is a zip of a few XML parts, and writing them directly is the only way the output reaches the response without a buffer.
The file is unchanged: the same sheets, columns, number formats and widths - apart from the empty cells the shared answer rule below now leaves blank on the Holder Export.
The Info sheet still opens first - sheet order is `workbook.xml`'s, not the zip's - and it is written last, so the row count it states is the number of rows actually streamed.

**One snapshot.**
The export reads inside a single read-only `REPEATABLE READ` transaction, so the file is exactly the roster at the moment it was generated, which is what its Info sheet claims.
Paging without a snapshot was rejected: a sale, reassignment or acceptance mid-export can move a row across a page boundary under the `holder` or `buyer` sort, and a roster that silently loses or repeats a person is ADR 0065's worst outcome.

**Concurrent exports are bounded per instance, at two.**
A streaming export holds one of the instance's ten database connections for as long as the client takes to download, up to the request timeout.
The export's own deadline sits just inside that timeout, and it is also set as the connection's write deadline, so a client that stops reading is let go when the export's time is up rather than holding its slot for as long as TCP keeps retrying.
A third concurrent export is refused at once with a retryable "an export is already being prepared" - a refusal about capacity, never about size.

**A failure mid-stream aborts the connection.**
Once the first byte is sent there is no status code left to change, so a failure after that point breaks the connection rather than finishing the file.
The zip's central directory is never written, so a truncated file cannot be opened; and the staff app reads the download as a blob, which rejects, so nothing is saved and the page shows an error.
A file silently missing its last rows - the reason ADR 0065 refused rather than truncated - still cannot occur.

**The audit line becomes two.**
`holder export started` is written before the first row, naming who took it, from which Organization and Event, under which honoured filters.
`holder export finished` records whether it completed or aborted, how many rows were produced, and why it aborted.
It is written on every way out once `started` has been, a panic included.
Both "completed" and the row count record what the server handed to the transport, not what the reader received.
The row count is the rows handed to the file's encoder: on an abort, the last of them may never have left the process.
"Completed" means the last byte of the file was handed to the connection, and nothing more.
Kernel, Cloud Run and proxy buffers can still hold a tail the reader never receives, so a completed line beside a download the reader saw fail is possible, and is not a contradiction.
An abort is recorded as a reason and an error class, never the error's own text, which for a client that went away names its address and port.
A completed export is logged at INFO and an aborted one at WARN, and both lines carry the request id, the key that joins them.
The start line is the only record that survives every way a stream can die - a deploy, a scale-down, the timeout - and personal data leaves with the first chunk, not with the last.
The search term is still never logged, on either line.

**One rule for what an answer looks like.**
The step that turns a Ticket Answer into a typed cell value - text, date, boolean or blank - becomes writer-independent and shared by both files, beside the already-shared question column builder.
The Holder Export and the Sales Export's per-Ticket sheet each keep only a thin serializer.
ADR 0065 accepted two overlapping files and refused two implementations; that still holds at the level of the rule, and only the bytes are written twice.

The rule also decides the two values a spreadsheet cannot take literally, the same way for both files:

- Empty text is an absent cell, never a text cell holding nothing, so a reader filtering on "is blank" finds it.
  It applies to every text cell of the Holder Export, and on the Sales Export only to the cells the shared rule writes: the per-Ticket sheet's Holder columns and its Answers.
  The rest of the Sales Export - its data sheet, and the per-Ticket sheet's `confirmation_ref` and `ticket_type_name` - still writes through excelize's `SetCellStr`, which writes empty text as an empty-string cell, exactly as before.
  The empty text that actually reaches a file is:
  - a Holder's first name, last name and email (`holder_email`), empty on every Ticket without an accepted Holder, and a name possibly empty on an accepted one; these are columns of both files, and both files already left them absent before, because both writers skipped an empty Holder value;
  - a buyer's first or last name (`customer_first_name`, `customer_last_name`), one of which a Manually Recorded Sale may leave empty; these are columns of the Holder Export and of the Sales Export's data sheet, not of its per-Ticket sheet.
  The other text columns are never empty: `assignment_state` is always one of its three states, a buyer's email is required on every way a Sale is made, `confirmation_ref`, `channel` and `ticket_type_name` always hold a value, and an empty text Answer is never stored, because `parseTextAnswer` refuses empty text.
- Text is cut to 32,767 UTF-16 code units, which is Excel's limit and what Excel counts.
  A character outside the Basic Multilingual Plane counts as two, and the cut never splits a surrogate pair.
  excelize's `SetCellStr` already cut text exactly this way, so this changes no cell's value; `TestTextCellCutsExactlyAsExcelizeDoes` pins it.

Only the empty-text rule changes a value a user downloads, only on the Holder Export, and only for an empty buyer first or last name.
There it deliberately goes against #658's "the file does not change" and #656's "Nothing a user downloads changes": before it, the Holder Export wrote an empty buyer first or last name as an empty-string cell, and it now leaves the cell absent.
It already left an empty Holder name and email blank.
A roster whose blanks mean one thing everywhere is worth one small change to it.
The Sales Export's data sheet is not brought into line: it still writes an empty buyer name as an empty-string cell, because #655's story 34 requires that file unchanged, so on buyer names the two files still disagree.
The Sales Export's values are unchanged, as story 34 requires.
Its per-Ticket sheet already left an empty Holder name and email blank.
One cell changes its encoding but not its value: a date Answer before 1900, which Excel has no serial for, is the text `1850-01-01T00:00:00Z` either way, but it was an inline string stamped with the date style and is now a shared string with no style.
Excel shows a text cell's text whatever number format it carries, so a reader sees the same thing.

**The Sales Export response now sends `Cache-Control: no-store`.**
The Holder Export always has, since its file is attendee personal data.
The Sales Export's file carries every buyer's name, email address and Tax ID, and, when it has a per-Ticket sheet, every accepted Holder's name and email address and every Ticket Answer.
This is a response header, not file content, so it changes nothing in the file.

## Considered options

**excelize's StreamWriter.**
Rejected: on an in-memory filesystem it moves the buffer rather than removing it, and still needs a cap, now counted in cells.

**StreamWriter spilling to a mounted Cloud Storage volume.**
Flat memory, but slow writes and new infrastructure for one export.

**Generating in the background into Cloud Storage and handing back a link.**
The robust answer if a roster ever outgrows 300s of streaming, and premature at today's sizes by more than an order of magnitude.

**A higher row cap on the in-memory writer.**
Needs several GiB of `api_memory` on every instance, leaves a number an organizer will eventually hit, and turns an OOM - which takes the instance down under every request in flight - from impossible into possible.

## Consequences

- The Holder Export and the Sales Export no longer share a workbook library.
  A change to the file's look is now made in two writers; a change to what an answer means is still made once.
- The "narrow your filters" refusal, its field error and its handling on the Holder List are deleted, along with the `COUNT(*)` that existed to produce it.
- A failed export is visible only as a failed download and the `finished` line; there is no error envelope to show once streaming has begun.
- The Sales Export keeps its 10,000-row cap, whose reason is symmetry with the Sale Import, not memory.
  That cap's memory cost has since been measured (#661): at the cap, with the widest plausible question set, one export peaked at 5,727 MiB resident, about 5.6 GiB, against an `api_memory` of 512Mi.
  A tenth of the cap does not fit either, at 590 MiB resident.
  What to do about it is still open, and this ADR does not decide it.
