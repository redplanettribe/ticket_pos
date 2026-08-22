-- The Answer: what one Ticket says in reply to one Ticket Question (#310,
-- ADR 0043, ADR 0044).
--
-- THIS SCHEMA SHIPS DARK, like migration 072's before it. Every route into an
-- Answer is behind TICKET_QUESTIONS_ENABLED, which is off, so an empty table is
-- the correct state of this migration in production and anybody who finds it
-- empty has found the feature working as designed (ADR 0045).
--
-- AN ANSWER BELONGS TO A TICKET. Not to the Ticket Sale, not to the buyer, not
-- to the Payment. A person buying four tickets is not assumed to know four
-- people's sizes, and one row per (sale, question) could not hold four different
-- ones — that is the option ADR 0043 rejected and the reason the Ticket became a
-- row at all. Answers held on a Payment during checkout are a different table
-- owned by a different ticket (#311); they are written ONTO these rows when the
-- sale commits, and nothing in this migration knows about them.
--
-- NO VERSION HISTORY AND NO AUTHOR. There is an updated_at and there is
-- deliberately no answered_by, no changed_by and no audit sibling table. Three
-- parties may supply an Answer — the buyer at checkout, whoever opens the Answer
-- Link, and Event Staff — and which of four friends ordered the wrong size is
-- not a dispute this product adjudicates. Recording the author would also mean
-- recording an identity for the Answer Link's holder, who is precisely the person
-- this platform collects nothing about (ADR 0044).
CREATE TABLE ticket_answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- ON DELETE CASCADE follows `tickets`, which cascades from
    -- `ticket_sale_lines` and ultimately from `events`. An Answer cannot outlive
    -- the Ticket it is a fact about; there is nothing left for it to be about.
    --
    -- NOTE WHAT THIS IS NOT: a Sale Reversal deletes nothing. A reversed Ticket
    -- Sale keeps its Tickets and they keep their Answers, exactly as the sale
    -- keeps its Sale Confirmation reference. The Answers simply stop being
    -- writable, which is a rule in catalog.AnswerWindow and not a cascade here.
    ticket_id UUID NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,

    -- ON DELETE CASCADE follows `ticket_questions`, which can only be reached by
    -- a cascade from a Ticket Type, which can only be deleted while its Event is
    -- a draft and nothing has been sold. A question is RETIRED and never
    -- deleted precisely so that the Answers under it keep reading, so in
    -- practice this cascade never fires against a real Answer.
    ticket_question_id UUID NOT NULL REFERENCES ticket_questions (id) ON DELETE CASCADE,

    -- THE FOUR TYPED COLUMNS. One per shape a non-choice Answer takes, and at
    -- most one of them is filled on any row — see the CHECK below.
    --
    -- FOUR COLUMNS RATHER THAN ONE TEXT OR ONE JSONB, and this is the decision
    -- this table most wants recorded. The PRD's promise for the export is that a
    -- number arrives "typed as a number" and a date "typed as a date", so that
    -- "how many larges do I order" is a pivot table. A single text column would
    -- make every one of those a string the export has to re-parse, with a second
    -- copy of the per-kind rules living in the exporter to do it — and the day a
    -- `number` question is asked to be summed or sorted in SQL, it sorts
    -- lexically and 10 comes before 9. JSONB would hold the types but not check
    -- them, and would make the same query a jsonb_typeof away.

    -- short_text and long_text. The caps that tell those two apart are
    -- catalog.MaxShortTextAnswerLength and MaxLongTextAnswerLength and are NOT
    -- CHECKed here, because the column cannot see the question's kind and a
    -- length rule that only half applies is worse than none. TEXT rather than
    -- VARCHAR(n) for the reason every other text column here is.
    text_value TEXT,

    -- number. NUMERIC WITHOUT A PRECISION OR SCALE, deliberately: NUMERIC(15, 6)
    -- would silently ROUND a seventh decimal place away, and an Answer that
    -- comes back different from what somebody typed is worse than one that was
    -- refused. The bounds are stated in Go (catalog.MaxNumberAnswerIntegerDigits
    -- and MaxNumberAnswerFractionDigits), where exceeding them is a refusal the
    -- caller can read. Unconstrained NUMERIC also preserves the trailing zero in
    -- `3.50`, which a number question asking a price or a measurement means.
    number_value NUMERIC,

    -- date. DATE and pointedly not TIMESTAMPTZ: a date Answer is a CALENDAR DATE
    -- — a birthday, a travel day — and giving it a time would mean giving it a
    -- zone, and a birthday that moves by a day depending on where the server is
    -- standing is exactly the bug this column type prevents.
    date_value DATE,

    -- checkbox. FALSE IS AN ANSWER and NULL is the absence of one: somebody who
    -- read "I will attend the dinner" and left it unticked has said no, which is
    -- a different fact from never having been asked. That second fact is an
    -- Outstanding Answer, and it is represented by there being no row here at
    -- all — never by a row with everything NULL.
    boolean_value BOOLEAN,

    -- When this Ticket first answered this question, and when the Answer last
    -- changed. UPDATED_AT IS THE WHOLE OF THE HISTORY THIS PLATFORM KEEPS: the
    -- PRD says "no version history and no record of who changed it", so a
    -- correction overwrites and leaves a timestamp, and that is all.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- ONE ANSWER PER (TICKET, TICKET QUESTION). The constraint IS the model: an
    -- Answer is what one Ticket says in reply to one Ticket Question, so a
    -- second row for the same pair would be two replies with nothing to say
    -- which is current. Re-answering is an UPDATE through this key, which is
    -- also what makes the upsert on the write path honest rather than a
    -- delete-then-insert that a crash could leave half done.
    UNIQUE (ticket_id, ticket_question_id),

    -- AT MOST ONE TYPED VALUE PER ROW. It cannot be EXACTLY one, because a
    -- choice Answer fills none of these — its value is the rows in
    -- ticket_answer_options below — and the CHECK cannot see them. So this
    -- refuses the failure mode it can see: a row claiming to be two kinds at
    -- once, which is what a bug in the per-kind writer would look like.
    CONSTRAINT ticket_answers_one_typed_value_ck CHECK (
        (text_value IS NOT NULL)::int
        + (number_value IS NOT NULL)::int
        + (date_value IS NOT NULL)::int
        + (boolean_value IS NOT NULL)::int <= 1
    )
);

-- "This Ticket's Answers", which is how the staff surface and the Answer Link
-- page both read: one Ticket, all of its questions. The UNIQUE above already
-- indexes (ticket_id, ticket_question_id), and this is that same leading column,
-- so no separate index on ticket_id is needed.

-- "Has this Ticket Question been answered by anybody" — the fact the kind freeze
-- is read against (catalog.TicketQuestionKindFrozen), and the fact #313's
-- Outstanding Answers view will count against. Without this index that EXISTS is
-- a sequential scan on every question edit.
CREATE INDEX ticket_answers_question_id_idx ON ticket_answers (ticket_question_id);

-- The Options one choice Answer picked, with the words each of them showed at
-- the time.
--
-- A CHILD TABLE RATHER THAN A COLUMN ON THE ANSWER, for two reasons that point
-- the same way. multi_choice must hold SEVERAL Options in ONE Answer, so a
-- single option_id column cannot express it and an array column could not carry
-- a foreign key to the Options it names. And every Answer needs its OWN snapshot
-- of the label, which is per chosen Option and not per Answer.
--
-- A row here is therefore "this Answer chose that Option, and this is what that
-- Option read when it was chosen". A single_choice Answer has exactly one; a
-- multi_choice Answer has one per box ticked.
CREATE TABLE ticket_answer_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    ticket_answer_id UUID NOT NULL REFERENCES ticket_answers (id) ON DELETE CASCADE,

    -- THE OPTION'S IDENTITY, which is emphatically not its label. This is what
    -- migration 072 gave Options their own id for: correcting `Mediun` to
    -- `Medium` is a one-column UPDATE over there that every row here follows
    -- automatically, forking nothing.
    --
    -- ON DELETE RESTRICT and not CASCADE. An Option is RETIRED, NEVER DELETED —
    -- gone from new lists, kept on the Tickets that chose it, and still given a
    -- column in the Sales Export — and this is the database refusing to be the
    -- thing that breaks that promise. Nothing in the application deletes an
    -- Option; if something ever tries, it fails here rather than silently
    -- erasing what people answered.
    ticket_question_option_id UUID NOT NULL REFERENCES ticket_question_options (id) ON DELETE RESTRICT,

    -- THE SNAPSHOT: the words this Option showed at the moment it was chosen.
    --
    -- This is the half of the Answer that the Option's id cannot supply, and it
    -- is redundant ON PURPOSE. CONTEXT.md: "each Answer keeps its own snapshot
    -- of the words the person actually read". If an Organization renames
    -- `Chicken` to `Chicken (halal)` in July, the person who ticked it in June
    -- ticked `Chicken`, and a support conversation about what they agreed to has
    -- to be able to say so. The CURRENT label is what the export header and
    -- every list show — that comes from the join — and this is what the person
    -- read.
    --
    -- NOTHING MAY EVER REWRITE IT. A rename does not touch this column, and any
    -- future backfill that "corrects" old snapshots to current labels is
    -- destroying the only record of what was shown.
    option_label_snapshot TEXT NOT NULL,

    -- The order the Options were chosen in, so a multi_choice Answer reads back
    -- the way it was given rather than in whatever order the rows return.
    sort_order INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- The same Option cannot be chosen twice in one Answer. Ticking a box twice
    -- is not a stronger answer.
    UNIQUE (ticket_answer_id, ticket_question_option_id)
);

-- Every read here is "this Answer's chosen Options", which the UNIQUE above
-- indexes on its leading column. This second index is for the other direction —
-- "is this Option chosen by anything", which is what makes the RESTRICT above
-- checkable without a scan, and what #314's per-Option export column counts.
CREATE INDEX ticket_answer_options_option_id_idx
    ON ticket_answer_options (ticket_question_option_id);
