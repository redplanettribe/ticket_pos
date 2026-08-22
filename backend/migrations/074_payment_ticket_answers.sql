-- The Answers a buyer gives at checkout, held on the PAYMENT until there is a
-- Ticket to put them on (#311, ADR 0044).
--
-- THIS SCHEMA SHIPS DARK, like migrations 072 and 073 before it. The checkout
-- form that fills it is behind TICKET_QUESTIONS_ENABLED, which is off, so an
-- empty table is the correct state of this migration in production and anybody
-- who finds it empty has found the feature working as designed (ADR 0045).
--
-- WHY THE ANSWERS RIDE THE PAYMENT AND NOT THE TICKET. Because at the moment
-- the buyer types them there is no Ticket. A Ticket is minted inside the
-- sale-commit transaction and only for an approved Payment (ADR 0043), and the
-- buyer answers a question BEFORE they are handed to the Payment Provider —
-- with a redirect, a hosted card form and an indefinite amount of human
-- hesitation between the two. Everything else the checkout form collects that
-- confirm cannot see again is snapshotted onto the Payment for exactly this
-- reason: the Tax ID, the phone, the Sale Locale, the affiliate attribution and
-- the consent answers (migrations 019, 027, 059 and 064). These are the sixth.
--
-- KEYED BY (PAYMENT LINE, INDEX), WHICH MIRRORS THE PRICE SNAPSHOT. A
-- `payment_line` says "three of this Ticket Type at this price"; the index says
-- which of those three this Answer is about. That pair survives the redirect,
-- and when the sale commits it lands on `tickets.ordinal`, which exists
-- precisely so that "written onto the minted Tickets IN ORDER" means something
-- (migration 070). Index n becomes ordinal n, and nothing in between has to
-- guess.
--
-- NOTHING HERE MAY EVER REFUSE OR DELAY A CHECKOUT, which is ADR 0044 and is
-- the reason this table has no NOT NULL it could trip over and no reference to
-- `required`. An Answer that does not parse, names a question this Ticket Type
-- does not ask, or points past the line's own quantity is DROPPED at capture and
-- never reaches these columns. The buyer keeps their tickets; the Organization
-- gets an Outstanding Answer, which is a debt it can chase and not a defect.
CREATE TABLE payment_ticket_answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The line, and therefore the Ticket Type, this Answer is about.
    --
    -- ON DELETE CASCADE follows `payment_lines`, which cascades from `payments`.
    -- An Answer held for a Payment cannot outlive the line that says what was
    -- being bought; without it the index below indexes nothing.
    --
    -- NOTE WHAT THIS CASCADE IS NOT. It is not the retention rule. Answers on a
    -- Payment that never reaches `approved` are purged 30 days after the Payment
    -- — never on `expired` alone, which can still flip to approved — and that is
    -- a swept job over these rows (#316), not a delete of the Payment. The
    -- Payment itself is kept forever; its Answers are not.
    payment_line_id UUID NOT NULL REFERENCES payment_lines (id) ON DELETE CASCADE,

    -- Which of the line's units this Answer is about, 1..quantity.
    --
    -- ONE-BASED, TO MATCH `tickets.ordinal`, which is the column it becomes. A
    -- zero-based index here would mean an off-by-one sitting between the buyer's
    -- form and the Ticket that carries their answer forever, and the two numbers
    -- meaning the same thing is the entire mechanism.
    --
    -- IT IS NOT CHECKED AGAINST `payment_lines.quantity` here, deliberately: a
    -- CHECK may not read a sibling row. An index past the quantity is refused at
    -- capture, and — because a refusal must never cost anybody a checkout — the
    -- commit path ALSO ignores any that got through, so a Ticket that does not
    -- exist cannot be answered by arithmetic.
    ticket_index INTEGER NOT NULL CHECK (ticket_index > 0),

    -- ON DELETE CASCADE follows `ticket_questions`, on the same terms migration
    -- 073 sets out: a question is RETIRED and never deleted, so in practice this
    -- cascade only ever fires for a Ticket Type deleted while its Event was a
    -- draft and nothing had been sold.
    ticket_question_id UUID NOT NULL REFERENCES ticket_questions (id) ON DELETE CASCADE,

    -- THE FOUR TYPED COLUMNS, one per shape a non-choice Answer takes, holding
    -- exactly what `ticket_answers` holds and in exactly the same types.
    --
    -- MIRRORED RATHER THAN NARROWED TO TEXT, and this is the decision this half
    -- of the table wants recorded. The commit path copies these columns onto a
    -- `ticket_answers` row; if this table stored a number as text, that copy
    -- would be a re-parse, and the per-kind rules would have to exist a second
    -- time to do it — which is precisely what catalog.ParseAnswer exists to
    -- prevent. Storing the same types means the write at commit is a copy and
    -- can have no opinion of its own. It also means Postgres refuses here, at
    -- capture, what it would otherwise refuse at commit — and a NUMERIC that
    -- fails to parse thirty minutes later, after the provider has taken the
    -- money, is an approved-without-sale incident over a t-shirt size.
    text_value TEXT,
    number_value NUMERIC,
    date_value DATE,
    boolean_value BOOLEAN,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- ONE ANSWER PER (LINE, INDEX, QUESTION), which is `ticket_answers`' UNIQUE
    -- said one redirect earlier. A second row for the same triple would be two
    -- replies with nothing to say which is current, and re-posting the checkout
    -- form is an UPDATE through this key rather than a second Answer.
    UNIQUE (payment_line_id, ticket_index, ticket_question_id),

    -- AT MOST ONE TYPED VALUE PER ROW, identical to
    -- ticket_answers_one_typed_value_ck and for the identical reason: it cannot
    -- be exactly one, because a choice Answer fills none of these and its value
    -- is the rows in payment_ticket_answer_options below.
    CONSTRAINT payment_ticket_answers_one_typed_value_ck CHECK (
        (text_value IS NOT NULL)::int
        + (number_value IS NOT NULL)::int
        + (date_value IS NOT NULL)::int
        + (boolean_value IS NOT NULL)::int <= 1
    )
);

-- Every read here is "this Payment's held Answers", reached line by line, which
-- the UNIQUE above already indexes on its leading column. No index on
-- ticket_question_id: nothing asks "has anybody answered this question on a
-- Payment" — the kind freeze reads `ticket_answers`, because a question whose
-- only replies are on abandoned Payments has not been answered by any Ticket.

-- The Options one held choice Answer picked, with the words each of them showed
-- at the time — `ticket_answer_options` one redirect earlier.
--
-- THE SNAPSHOT IS TAKEN HERE AND CARRIED, NOT RETAKEN AT COMMIT. The buyer read
-- `Chicken` on the checkout form; if the Organization renames it to
-- `Chicken (halal)` while the buyer is on the provider's payment page, the words
-- they actually read are the ones in this column and re-reading the Option at
-- commit would quietly replace them with words nobody was shown. That the
-- snapshot records what was READ, not what is current, is the whole reason it is
-- stored (migration 073), and the redirect is the one gap in this product where
-- the two can genuinely differ.
CREATE TABLE payment_ticket_answer_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_ticket_answer_id UUID NOT NULL
        REFERENCES payment_ticket_answers (id) ON DELETE CASCADE,

    -- The Option's IDENTITY, which is not its label. ON DELETE RESTRICT for the
    -- reason `ticket_answer_options` has it: an Option is retired, never
    -- deleted, and the database refuses to be the thing that breaks that.
    ticket_question_option_id UUID NOT NULL
        REFERENCES ticket_question_options (id) ON DELETE RESTRICT,

    -- The words this Option showed at the moment it was chosen. Copied onto the
    -- Ticket's Answer verbatim when the sale commits; nothing ever rewrites it.
    option_label_snapshot TEXT NOT NULL,

    -- The order the Options were chosen in, so a multi_choice Answer reaches the
    -- Ticket reading the way it was given.
    sort_order INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- The same Option cannot be chosen twice in one held Answer.
    UNIQUE (payment_ticket_answer_id, ticket_question_option_id)
);

-- The other direction — "is this Option chosen by anything" — which is what
-- makes the RESTRICT above checkable without a sequential scan.
CREATE INDEX payment_ticket_answer_options_option_id_idx
    ON payment_ticket_answer_options (ticket_question_option_id);
