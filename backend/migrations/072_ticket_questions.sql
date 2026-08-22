-- The Ticket Question and its Options: what an Organization wants to know about
-- whoever will hold one of a Ticket Type's tickets (#309, ADR 0045).
--
-- THIS SCHEMA SHIPS DARK. The authoring surface is behind a flag that is off
-- (ADR 0045), and nothing can produce an Answer yet — there is no Answer table
-- in this migration and no customer-facing surface reads either table. What
-- lands here is the mechanism; collection does not begin until a Policy Version
-- describing it publishes. Two empty tables are the correct state of this
-- migration in production for now, and anybody who finds them empty has found
-- the feature working as designed.
--
-- A Ticket Question belongs to ONE Ticket Type, never to an Event and never to
-- an Organization. The same wording on three Ticket Types is three Ticket
-- Questions that happen to read alike, which is the shape CONTEXT.md chose: a
-- shared question would have to answer "what happens when one Ticket Type
-- retires it", and an Organization that wants the same question twice is better
-- served by copying six words than by a shared definition it cannot safely edit.
CREATE TABLE ticket_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- ON DELETE CASCADE, matching ticket_type_promotions: a Ticket Type can only
    -- be deleted while its Event is a draft and nothing has been sold, so a
    -- cascade here can never take a question that any Ticket answered.
    ticket_type_id UUID NOT NULL REFERENCES ticket_types (id) ON DELETE CASCADE,

    -- The question AS COINED. The Organization's own words, read identically on
    -- an `en` and an `es` Storefront page exactly as a Custom Tag is (ADR 0027)
    -- — which is why there is one label column and not one per Locale, and why
    -- nothing normalizes it beyond trimming. Only page chrome follows the
    -- Locale.
    label TEXT NOT NULL,

    -- One of the seven kinds. CHECKed here as well as parsed in Go because the
    -- set is closed, small and unlikely to move: the export, the checkout form
    -- and the Answer Link page all branch on it, and a row with an eighth value
    -- would be a shape none of them can draw.
    kind TEXT NOT NULL CHECK (
        kind IN (
            'short_text', 'long_text', 'single_choice', 'multi_choice',
            'number', 'date', 'checkbox'
        )
    ),

    -- Required, whose ONLY effect is producing an Outstanding Answer. It is not
    -- a constraint on anything and must never become one: nothing about a Ticket
    -- Question ever blocks a checkout, a door sale or a Sale Import. It is a
    -- debt the Organization can see and chase, not a defect.
    required BOOLEAN NOT NULL DEFAULT FALSE,

    -- When the question is put to somebody. v1 only ever writes 'at_checkout';
    -- 'after_purchase' is CHECKed in from the start so that honouring the
    -- Organization's choice later is a code change rather than an Answer
    -- migration.
    timing TEXT NOT NULL DEFAULT 'at_checkout' CHECK (timing IN ('at_checkout', 'after_purchase')),

    -- The order the questions are asked in, resequenced 0..n by the reorder
    -- endpoint. Ties break on created_at, as ticket_types do.
    sort_order INTEGER NOT NULL DEFAULT 0,

    -- RETIRED, NEVER DELETED — the same rule its Options follow, and for a
    -- reason that only bites later: a question's kind is frozen once any Answer
    -- exists, so an Organization that typed the wrong kind retires the question
    -- and adds another. The Answers already given must survive that, and a
    -- DELETE would take them. NULL is live.
    retired_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every read is "the questions on this Ticket Type, in order", so the index
-- carries the sort with it.
CREATE INDEX ticket_questions_ticket_type_id_idx ON ticket_questions (ticket_type_id, sort_order);

-- One selectable value of a choice Ticket Question — S, M, L.
--
-- A TABLE RATHER THAN A TEXT[] OR JSONB COLUMN ON THE QUESTION, and that is the
-- decision this half of the migration records. An Option needs identity
-- INDEPENDENT OF ITS LABEL: an Answer refers to the Option it chose, and
-- correcting `Mediun` to `Medium` must not fork the Answers already given under
-- the typo. An array of strings makes the label the identity, so every rename is
-- either a data migration over the Answers or a silent loss of them. A row with
-- its own id makes a rename a one-column UPDATE that nothing else notices.
--
-- The same property is what makes retirement cheap: a retired Option is a row
-- with a timestamp, still joinable from every Answer that chose it and still
-- entitled to its column in the Sales Export.
CREATE TABLE ticket_question_options (
    -- The stable identity described above. This is what an Answer will point at,
    -- and it never changes for the life of the Option.
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    ticket_question_id UUID NOT NULL REFERENCES ticket_questions (id) ON DELETE CASCADE,

    -- The Option AS COINED, on the same terms as the question's label above. The
    -- CURRENT label is what the export header shows; an Answer is expected to
    -- keep its own snapshot of the words the person actually read, which is the
    -- Answer's business rather than this table's.
    label TEXT NOT NULL,

    sort_order INTEGER NOT NULL DEFAULT 0,

    -- Retired, never deleted: gone from new lists, kept on the Tickets that
    -- chose it, and still given an export column. NULL is live.
    --
    -- There is deliberately NO unique constraint on (ticket_question_id, label).
    -- Two live Options reading alike is an authoring mistake the surface can
    -- warn about, but a retired `Large` and a new `Large` are a perfectly
    -- ordinary sequence — an Organization retiring an Option and reinstating it
    -- a season later must not be refused by the database, and the two rows are
    -- genuinely different Options because different Tickets chose them.
    retired_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX ticket_question_options_question_id_idx
    ON ticket_question_options (ticket_question_id, sort_order);

-- THE TWENTY-OPTION CAP IS NOT A CONSTRAINT HERE, deliberately. It counts LIVE
-- Options — a retired one has left every new list, so counting it would mean an
-- Organization that corrected its sizes twice eventually could not add another —
-- and a partial count across sibling rows is not something a CHECK can state.
-- The catalog service enforces it on the one path that inserts, the way the
-- Promotional-Price invariant in migration 033 is enforced for the same reason.
