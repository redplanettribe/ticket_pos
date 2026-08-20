# Event Ticketing

A point-of-sale system for organizations to create events, define ticket types, and sell tickets online or in person, with programmatic access for Integration Partners.

## Organization & access

**Organization**:
A group such as a promoter, venue, or company that owns Events and has members.
Has an optional Logo.
Read as **Organizer** wherever the Storefront speaks to a Customer about one — "events from organizers everywhere", "contact the organizer". The public has no use for the word Organization, and Organizer is the word it has been given on every public page; staff surfaces name the entity Organization, as do identifiers everywhere. This is the Organization seen from outside, not a second concept — so prose may name it Organizer when it is that outside view being discussed — and it does not license calling a Member an organizer, see Event Owner.
In Spanish, **Organización** — and never *Organizador*, which is this same entity's public word and belongs to Customer-facing surfaces alone. The distinction the English holds is the one the Spanish must hold.
_Avoid_: Account, workspace, team. In Spanish: Organizador, cuenta, espacio de trabajo, equipo

**Logo**:
An Organization's brand image, set by an Org Admin and shown wherever the Organization is represented to staff or Customers (e.g. the Storefront).
Distinct from an Event's cover image, which represents a single Event rather than the Organization.
_Avoid_: Icon, avatar, brand image

**Support WhatsApp**:
An Organization's optional WhatsApp number, set by an Org Admin, published on the Organization's public Event pages as a link a Customer can message for support.
Stored in canonical E.164 form under the same rule as a Customer's phone number.
_Avoid_: Support number, helpline, contact number, WhatsApp link

**Member**:
A person belonging to an Organization who may hold a role such as Org Admin, Event Owner, or Event Staff.
In Spanish, **Miembro**.
_Avoid_: User, account, teammate. In Spanish: Usuario, integrante, colaborador

**Staff Session**:
A server-side staff sign-in record tied to an email address.
The session may or may not have an active Member selected.
Signing in proves email ownership; acting in an Organization requires an active Member on the session.
Distinct from a Customer Session, with which it shares nothing but its Proof of Email Ownership methods.
_Avoid_: Session, login, token, cookie

**Active Member**:
The Member record currently selected on a Staff Session.
Staff API calls and workflows are scoped to the active Member's Organization and role.
_Avoid_: Current user, active org, tenant context

**Org Admin**:
A member of an Organization with full authority over all Events in that Organization, including member and event assignment management.
For now, equivalent in scope to an Event Owner on any single Event.
In Spanish, **Administrador de la organización**.
_Avoid_: Organization owner, super admin. In Spanish: Admin, Administrador del organizador, Dueño de la organización

**Event assignment**:
A grant linking a Member to an Event with a role of Event Owner or Event Staff.
Required for a non-Org Admin Member to act on that Event; Org Admins do not need assignments.
_Avoid_: Permission, ACL, grant

**Event Owner**:
A member with full authority over a specific Event.
For now, equivalent in scope to an Org Admin within that Event.
In Spanish, **Responsable del evento**.
_Avoid_: Organizer, host, creator. Organizer is barred here because it names the whole Organization to the public, never a person holding a role inside one. In Spanish: Organizador, Propietario del evento, Dueño del evento, Anfitrión

**Event Staff**:
A member granted access to manage an Event's catalog and sell tickets for that Event.
In Spanish, **Personal del evento**.
_Avoid_: Ticket Type Manager, collaborator, delegate. In Spanish: Staff del evento, Equipo del evento, Colaborador del evento

**Integration Partner**:
An external service that manages Events, Ticket Types, and sales on behalf of an Organization.
For now, equivalent in scope to an Org Admin across all of that Organization's Events.
Distinct from an External Platform, which is where sales happen outside this system before they are imported.
_Avoid_: API client, developer, third-party app

**Integration**:
An authorization granting an Integration Partner org-wide access to an Organization's Events, catalog, and sales.
_Avoid_: API key, connection, webhook

**Platform Operator**:
A person who runs the platform itself, with read authority spanning every Organization and the authority to assert what the platform's money did: recording Payouts — whether answering a Payout Request or not — and recording an Operator Reversal against any Online Sale.
Granted by presence on the operator allowlist, keyed by email; exercised through an ordinary Staff Session.
Orthogonal to Membership — a Platform Operator need not be a Member of any Organization, and being an Org Admin grants no operator authority.
_Avoid_: Platform admin, super admin, site admin, root

**Operator Dashboard**:
The Platform Operator's surface inside the staff app, entered by switching to Platform rather than to an Organization: it holds every Organization with its Events and Withdrawable Balance, platform revenue totals per currency, where Payouts are recorded, where the outstanding Payout Requests of every Organization queue up to be answered, and where a Ticket Sale is looked up by its Sale Confirmation reference — across every Organization — to be read or reversed as an Operator Reversal.
A whole surface, not a single screen: what it holds is spread across pages of its own, and it is the authority that makes them one thing, not the layout.
Does not exist for non-operators.
_Avoid_: Admin panel, back office, console

**Staff Locale**:
The language the staff app is written in, and the language staff mail is written in, for the person signed in. English or Ecuadorian Spanish, the two the Storefront serves, and its Spanish is written in usted as Customer mail is (ADR 0033).
A property of a person rather than of a page or of a Membership, keyed on the email address staff identity already runs on, so it follows somebody across devices and across Organizations, and a Platform Operator who is a Member of nowhere holds one too. Chosen from a switcher in the app shell — a control about you, not about the Organization — and absent, rather than English, until somebody has stated one.
One term where the Storefront needs two, because a page cannot reach an email and a person can (ADR 0041): it words the screen and the mail together, so the two can never disagree.
Decides words and the marks around numbers and dates, and nothing about time or money, under the same rule a Locale follows.
_Avoid_: Locale (which is the Storefront's page property), Member Locale, staff language, UI language, preferred language

## Catalog

**Event**:
A scheduled occurrence belonging to an Organization, for which tickets are sold.
Has scheduling (start and optional end, timezone, venue), optional markdown description, Cover Image, and Cover Video, and a lifecycle status.
_Avoid_: Show, gig, occurrence

**Cover Image**:
An Event's optional still image, shown on Storefront listings, the Event page hero, and link previews.
Required before a Cover Video may be attached, and cannot be removed while one is; it serves as the Cover Video's poster.
Distinct from a Logo, which represents the Organization rather than a single Event.
_Avoid_: Banner, thumbnail, hero image, photo

**Cover Video**:
An Event's optional short looping video, shown in the Event page hero in place of the Cover Image once it has loaded.
Requires a Cover Image to exist as its poster; appears only in the hero — listings and link previews always use the Cover Image.
_Avoid_: Trailer, promo video, hero video, clip

**Event status**:
Where an Event is in its lifecycle: `draft` (being prepared), `published` (visible and sellable when sales exist), or `cancelled` (terminal; no longer active).
Publishing requires a way in — Ticket Types or a Registration Link — alongside the Event's own description; which of the two it is is settled while `draft` and cannot change afterwards.
Governs whether an Event is reachable at all; distinct from Discoverable, which governs whether a reachable Event advertises itself in listings.
_Avoid_: State, visibility flag, active/inactive

**Discoverable**:
A per-Event flag controlling whether a `published` Event appears in public listings — the Organization page and the global explorer — or is reachable by direct link only.
Only a `published` Event can be Discoverable; `draft` and `cancelled` Events are never listed. A non-Discoverable published Event is still fully sellable to anyone who has its link.
Any Member of the Organization may set an Event's Discoverable flag; every other event edit remains an Org Admin action.
In UI copy only, staff surfaces render the flag as "Listed" / "Not listed", naming the Storefront listing the Event does or does not appear in; code and prose keep the term Discoverable.
_Avoid_: Public/private, hidden, unlisted, featured

**Storefront listing**:
A public surface that lists Discoverable Events: the Organization page (`/{orgSlug}`, one Organization's Events) or the global explorer (`/`, Discoverable Events across all Organizations). Distinct from the Event page, which is the single-Event product and checkout surface.
_Avoid_: Catalog, index, feed, directory

**Timeline**:
The global explorer's presentation: one chronological list of Discoverable Events, soonest first, grouped into Day Buckets. The explorer's only view — there is no alternate grid or list mode.
_Avoid_: Feed, agenda, schedule, list view, calendar

**Day Bucket**:
A date group in the Timeline, keyed by the calendar date an Event starts in its own timezone — so a bucket's header can never contradict the date shown on its Events. An Event appears in exactly one place in the Timeline: the Day Bucket it starts in, or the Ongoing group once it has begun. Whether a bucket is labelled "Today" or "Tomorrow" is judged against the platform wall clock (Ecuador time), not the viewer's.
_Avoid_: Section, day group, date header

**Ongoing**:
The Timeline group above the Day Buckets holding every Event that has started but not yet ended — a multi-day Event mid-run or tonight's Event during its own runtime. Membership is a pure instant comparison (start passed, end not reached); an Event with no end never appears here, since it leaves the Timeline at its start. Cards in this group show their full start date, since the group header names no date.
_Avoid_: Live, in progress, happening now, started

**External Registration**:
An Event that sells no tickets and sends its audience to a third-party site to sign up — a Luma page, an Eventbrite listing, a form, the Organization's own site.
The alternative to selling Ticket Types, never a companion to it: an Event does one or the other, chosen while `draft` and fixed once `published`. Such an Event is otherwise an ordinary Event — it is described, Tagged, Discoverable, and promotable through Affiliate Links — but it has no capacity, is never sold out, and produces no Ticket Sale, so it reaches no Payment, Platform Fee, Net Proceeds, balance or Payout.
_Avoid_: External ticketing, off-platform sales, third-party event, RSVP

**Registration Link**:
The URL an Event with External Registration sends its audience to, and the only way into such an Event.
Editable while `published`, since a typo or a rescheduled registration page must be fixable, but never emptied. It carries no vendor identity: the platform stores a URL, not a relationship with whoever is on the other end.
Shows its clicks — never registrations and never people. The platform loses sight of the buyer at the link and never learns whether they signed up; only the site on the other side knows that.
_Avoid_: Affiliate Link, external ticket link, signup URL, RSVP link, registrations (as the counter's name)

**Ticket Type**:
A purchasable ticket category belonging to an Event, with a name, description, List Price, capacity, and display order.
_Avoid_: Ticket tier, ticket class, fare, SKU

**Free Ticket Type**:
A Ticket Type an Organization prices at zero.
Listed, selected, and counted against capacity exactly like any other, but a checkout that totals nothing never reaches a Payment Provider.
_Avoid_: Free tier, complimentary ticket, comp, RSVP, giveaway

**List Price**:
The standing price an Organization sets on a Ticket Type — what it costs whenever no Promotion is live.
The base the Platform Fee and Fee Handling arithmetic start from, and the struck-through figure shown beside a Promotional Price.
_Avoid_: Price (unqualified), regular price, base price, original price, full price

**Promotion**:
A time-boxed price override on a single Ticket Type: a Promotional Price that applies from an optional start until a required end, read in the Event's timezone, after which the List Price silently resumes.
At most one per Ticket Type at a time; editable and removable while live, and holding across every Sales Channel that prices from the catalog. It promotes a Ticket Type's price, not an Organization — unrelated to a promoter.
_Avoid_: Discount, coupon, voucher, offer, deal, flash sale, early bird, sale

**Promotional Price**:
The price a Promotion charges while it is live — an absolute amount, not a percentage, always strictly below the List Price and allowed to be zero.
Takes the List Price's place in all fee arithmetic, so the Platform Fee is a share of what the Customer actually paid; any "X% off" shown is derived for display.
_Avoid_: Discounted price, sale price, special price, offer price

**Purchase Limit**:
The most of one Ticket Type a single Customer may hold at once — unset on most Ticket Types, and the reason a Free Ticket Type is not handed out a hundred at a time.
Counted the way capacity is: a Customer's active Ticket Sales plus their live Capacity Holds, so a Sale Reversal returns it and an abandoned Payment releases it. Keyed on the Customer, which makes it a deterrent against taking too many rather than a defence against someone minting identities.
Governs future checkouts only: lowering it never unmakes a Ticket Sale, so a Customer may hold more than the current limit. Distinct from capacity, which is the Event-wide stock rather than one person's share of it.
Also refuses the offending rows of a Sale Import, counting the rows of one file against each other, so importing history recorded before the limit existed means raising or clearing it first.
_Avoid_: Quota, cap, max quantity, rate limit, one-per-person, purchase restriction

**Tag**:
A discovery facet an Event can wear, describing what kind of Event it is (e.g. "Music", "Workshop", "Techno"). An Event may carry several Tags, and Tags live in one system-wide shared pool reused across all Organizations. Aids Storefront discovery. Distinct from Ticket Type, which is a purchasable category within a single Event.
_Avoid_: Category, genre, label, keyword

**Preset Tag**:
A curated Tag seeded by the system and always offered as a filter in the global explorer's tag chip bar. The curated tier of the shared Tag pool.
Being the system's own word for a kind of Event, it is read in the Locale of the Storefront page it appears on — wherever it appears, chip or badge. A Preset Tag the Storefront has no words for is read in English rather than left blank.
_Avoid_: Default tag, official tag, built-in category

**Custom Tag**:
A Tag coined by an Organization when no existing Tag fits. It joins the same shared pool as Preset Tags and aids discovery through search and on the Organization and Event pages, but is not offered as a filter chip in the global explorer unless later promoted to a Preset Tag.
Being the Organization's own word, it reads the same in every Locale — as coined. So a Spanish page can show a Preset Tag in Spanish beside a Custom Tag in the language it was typed in; the first is a kind of Event this product names for itself, the second is a name somebody else chose.
_Avoid_: User tag, private tag, org tag

## Sales

**Customer**:
A person who buys a ticket.
Identified by a platform-global email — one Customer spans every Organization they have bought from.
A Customer record is created by any Ticket Sale on any Sales Channel, whether or not the person has ever signed in, or by a first sign-in for an email no sale has reached; every Ticket Sale belongs to one.
Holds what the person asserts about themselves (their current name and Tax ID, later their preferences), while each Ticket Sale keeps its own immutable record of what was transacted. First and last name are stored separately; where a single display name is needed they are joined as "First Last".
_Avoid_: Buyer, purchaser, account, user, full name

**Ticket Sale**:
A completed transaction in which tickets of one or more Ticket Types are sold, such as a cart checkout.
_Avoid_: Order, purchase, transaction

**Ticket Sale Line**:
One Ticket Type and the quantity sold within a Ticket Sale.
_Avoid_: Line item, cart item

**Tickets Sold**:
How many tickets an Event has moved: the quantities of its Ticket Sale Lines summed across active Ticket Sales, on every Sales Channel — a ticket sold at the door fills a seat exactly as one sold online does.
Answers "how many people are coming", where a count of Ticket Sales answers "how many times did somebody check out". One Ticket Sale of four tickets is 1 sale and 4 Tickets Sold, so neither figure can be read off the other and a surface showing one of them has to say which it is. Dropped by a Sale Reversal whole-Sale, like every other figure.
Not a count of people: a ticket is a thing sold, and the platform does not yet know who walks in on it — which is why Tickets Sold is the honest name for the closest answer the model has.
Summable at any grain the question needs: a day of an Event, a Ticket Type, an Event entire.
_Avoid_: Sales (as a word for tickets), attendees, headcount, seats, admissions

**Sales Channel**:
The medium through which a Ticket Sale is recorded: `online`, `in_person`, or `import`. Distinct from Sales Source, which further qualifies where an imported sale originated.
_Avoid_: Sale type, payment method

**Storefront**:
The public-facing channel through which a Customer completes an Online Sale.
_Avoid_: Shop, web store, e-commerce site

**Locale**:
The language-and-region identity a Storefront page is rendered under, carried explicitly in the URL (`/{locale}/...`) so that an address always says which language it serves.
The Storefront's term and only the Storefront's: what the staff app is written in is a Staff Locale, which is a property of the person signed in rather than of a page and carried in no address (ADR 0041).
Decides words and the marks around numbers and dates. Decides nothing about time or money: an Event's times are drawn in the Event's own timezone and the Reversal Window's cutoff in Ecuador's, and a Ticket Type's currency is the Organization's — none of them follows the reader's language.
A page's Locale comes from its address alone, never from the reader's browser or cookies; those only choose which Locale an address naming none redirects to.
A property of a page, so it cannot reach anything written outside one: mail has no address to carry a Locale, and mail to a Customer is written in the recipient's Mail Locale instead.
_Avoid_: Language (as the whole concept), region, translation, i18n, culture

**Online Sale**:
A Ticket Sale completed through the Storefront, recorded the moment its Payment is approved.
_Avoid_: Web sale, e-commerce sale

**In-Person Sale**:
A Ticket Sale completed at a physical point of sale by Event Staff.
_Avoid_: Door sale, box office sale, walk-up sale

**Payment Method**:
How a Ticket Sale was paid. Recorded for Direct Sales, where the value is `cash` or `transfer`, and for Online Sales, where the value names the Payment Provider that collected the money (`payphone` at launch) or is `free` when there was nothing to collect. Modeled as an extensible set so further methods can be added later.
_Avoid_: Payment type, tender, channel

**Payment Provider**:
An external service that collects money from a Customer on the platform's behalf during an Online Sale — PayPhone at launch. The platform holds the single merchant account with each Payment Provider; Organizations are settled outside the system. Distinct from an External Platform, which sells tickets itself, and from an Integration Partner, which manages the system programmatically.
_Avoid_: Gateway, processor, PSP, vendor

**Payment**:
A Customer's attempt to settle a checkout — through a Payment Provider when there is money to collect, and by the platform itself when the checkout totals nothing. A Payment that has money to collect begins `pending` when checkout starts and ends `approved` (the moment its Ticket Sale is recorded), `failed` (declined or cancelled), or `expired` (abandoned); one that has nothing to collect is born `approved` and never waits on anybody. A Ticket Sale exists only for an approved Payment; a Payment that never completes never becomes a sale and never appears in the Sales list. Approved therefore means the checkout is settled, not that money moved.
_Avoid_: Transaction, charge, checkout session, payment intent, order

**Capacity Hold**:
The claim a pending Payment places on Ticket Type capacity so a Customer cannot pay for tickets that sold out while they were paying. Derived from the pending Payments themselves — a Payment younger than the hold window holds its quantities; an older one has lapsed. Released by the Payment ending in any state or by the window passing.
_Avoid_: Reservation, cart lock, inventory block

**Sale Confirmation**:
A per-Ticket-Sale receipt carrying a human-readable reference code (e.g. `TP-3F9K2`), emailed to the Customer whenever a Ticket Sale is recorded. Not a per-attendee admission ticket; the model leaves room to attach individual tickets later.
_Avoid_: Ticket, receipt, order confirmation

**Sale Reversal**:
The voiding of a recorded Ticket Sale: its tickets cease to exist, its capacity returns to the Ticket Type, and any money collected is returned to the Customer.
Reachable by three routes, and the sale records which one voided it: the Customer on their own Online Sale within the Reversal Window, staff through a Sale Import undo, and a Platform Operator through an Operator Reversal. Always whole-Sale — no part of a Ticket Sale can be reversed on its own.
An outcome, not an ask. The Customer's route passes through a Reversal Request, and becomes a Sale Reversal only once the Payment Provider has confirmed the money went back; a Reversal Request that is refused never becomes one.
A reversed Ticket Sale is never deleted: it keeps its Sale Confirmation reference and stays visible to both the Customer and the Organization, and it stops counting toward Net Proceeds, the Withdrawable Balance, and platform revenue — except for a Platform Fee an Operator Reversal said the platform kept.
_Avoid_: Refund, cancellation, void, chargeback

**Reversal Request**:
A Customer's ask to undo their own Online Sale, recorded the moment they ask and pursued by the platform until the Payment Provider gives a definite answer. It ends as a Sale Reversal, as a refusal that leaves the sale exactly as it was, or as an Unresolved Reversal.
It exists because the Payment Provider does not always answer in time, and an unanswered reversal is not a failure to report but a question still open: until it is settled the Ticket Sale stays active and its capacity stays held, because no money is known to have moved. Only a Customer reversing a paid Online Sale makes one — a free Online Sale has no provider to wait on, and neither an Operator Reversal nor a Sale Import undo asks anybody's permission.
_Avoid_: Pending reversal, reversal attempt, refund request

**Reversal Reconciler**:
The platform asking the Payment Provider again what became of a Reversal Request it never answered, until the answer is definite or the platform gives up. It is the platform doing for itself what the Customer would otherwise do by pressing Undo a second time.
It only ever finds out; it never re-decides. A Reversal Request that was inside the Reversal Window when it was made stays authorised however long the answer takes.
It also finishes a reversal the Payment Provider agreed to and the platform failed to record: there the answer is already known, so it retries the local commit alone and asks nobody anything.
_Avoid_: Retry job, reversal worker, refund poller

**Unresolved Reversal**:
A Reversal Request the Reversal Reconciler gave up on with the answer still unknown — the money may or may not have gone back, and only the Payment Provider's own dashboard can say. Awaits a Platform Operator, who settles it as an Operator Reversal if the money did in fact leave.
The Customer is told nothing, because there is nothing true to tell them yet.
One kind is not unknown at all: a reversal the provider agreed to whose local commit never landed. The money went back, and the Operator's move is to void the sale rather than to look anything up.
_Avoid_: Failed reversal, stuck refund, orphaned reversal

**Operator Reversal**:
A Sale Reversal a Platform Operator records after refunding a buyer off-platform — by hand in the Payment Provider's dashboard, or by bank transfer the platform never saw. A record of money that already moved, like a Payout: no Payment Provider is involved and none is asked to confirm it.
Available on any active Online Sale, paid or free, whether or not its Reversal Window has passed — being past it is the reason the route exists. Not available on sales from other Sales Channels; an imported sale is undone through its Sale Import.
Remembers who asserted it and when, alongside an optional note, what the buyer actually got back, and whether the platform kept its Platform Fee and Fee IVA — the last three being the operator's own statement, absent rather than zero on a free Online Sale. A kept fee stays in platform revenue; the Organization's figures drop the sale like any other reversal.
Irreversible, and invisible to the Organization beyond the sale showing as reversed by the platform.
_Avoid_: Manual refund, admin refund, force reversal, out-of-band reversal

**Reversal Window**:
The period during which a Customer may reverse their own Online Sale: from the moment its Payment is approved until the earlier of 20:00 Ecuador time on the day of purchase, or the Event's start.
A platform rule rather than a provider one — it applies equally to a free Online Sale, where there is no money to return. That the launch Payment Provider happens to accept reversals over the same period is a fact about that provider, not the definition.
Ends early at the Event's start because the platform has no record of attendance and cannot otherwise tell a change of mind from a completed visit.
Governs when a Customer may ask, and nothing else. How long the platform then takes to learn what the Payment Provider did is not bounded by it: a Reversal Request made a minute before the cutoff is settled whenever the answer arrives, however long after the Window has closed.
_Avoid_: Refund period, cooling-off period, grace period, cancellation policy

**Platform Fee**:
The platform's commission on a Ticket Sale — a percentage (10% at launch) of the ticket price the Organization set. Always charged to the Organization by withholding from its proceeds, never to the Customer; Fee Handling only decides whether the buyer price is raised to cover it. The tickets themselves are the Organization's sale, and their taxation stays outside the system.
_Avoid_: Commission, service charge, take rate

**Fee IVA**:
Ecuadorian value-added tax (15% at launch) levied on the Platform Fee, because the fee is the platform's taxable service. Not a tax on the ticket itself — ticket-level tax remains the Organization's off-platform concern.
_Avoid_: Tax, VAT on tickets, sales tax

**Fee Handling**:
A per-Event choice of whether the buyer price is raised to cover the Platform Fee and its Fee IVA: `pass_on` (the price is raised by exactly fee + Fee IVA, so the Organization nets the price it set; the default) or `absorb` (the price stays as the Organization set it, so the Organization nets that price less fee and Fee IVA). It never changes who is charged — the fee is withheld from the Organization either way. One switch: the fee and its IVA always travel together. A change takes effect on future checkouts only; a Payment already in flight keeps the amounts it snapshotted.
_Avoid_: Fee mode, pricing mode, pass-through flag

**Net Proceeds**:
What an Online Sale leaves for the Organization once the Platform Fee and Fee IVA are withheld: the amount collected minus both. Under `pass_on` Fee Handling this equals exactly the ticket price the Organization set. Only Online Sales produce Net Proceeds — money from other Sales Channels never passes through the platform.
_Avoid_: Net revenue, earnings, take-home

**Takings**:
What a Ticket Sale earned the Organization, on whatever Sales Channel it sold: the Net Proceeds of an Online Sale, and the full price of a sale the platform took no cut of. Answers "what did this Event make", where Net Proceeds answers "what will the platform hand over" — so under `pass_on` Fee Handling, the default, Takings is exactly the price the Organization set, wherever the ticket sold (ADR 0040).
Stated in the Organization's currency, and dropped by a Sale Reversal like every other money figure. Not a claim on the platform: an Organization can never ask to be paid its Takings, because the door money and the imported money are already in its own pocket — the Withdrawable Balance is what the platform owes.
Summable at any grain the question needs: a day of an Event, a Ticket Type, an Event entire.
_Avoid_: Revenue, gross, earnings, income, sales (as a word for money), Net Proceeds (as a loose synonym)

**Sales Trends**:
The staff surface for reading how an Event's sales moved over time, split by Ticket Type and shown twice over — once counting Tickets Sold and once counting Takings — in one of two views chosen together for both figures: the **Daily view**, each day's own count, and the **Cumulative view**, the running total up to each day.
The Daily view answers which days sold and what sold on them; the Cumulative view answers how far the Event has come and when it accelerated; the Sales list answers what one Ticket Sale was. Reads a sale on the day it was made rather than the day it was recorded, so a Sale Import of last year's history lands in last year.
Restricted to Org Admins and Event Owners, the guard the Event's money already carries, rather than the Sales list's.
_Avoid_: Report, analytics, dashboard, sales graph, chart (as the surface's name). For the Cumulative view: burn-up, burn down, running total (as its name)

**Payout**:
A recorded settlement in which the platform transfers accumulated Net Proceeds to an Organization. Recorded per Organization — money is settled with the Organization, not with individual Events. A Platform Operator records it from the Operator Dashboard after settling off-platform, and the record remembers who recorded it. A record of money that already moved, so recording is never refused for exceeding the Withdrawable Balance — the balance simply goes negative and says so.
May answer a Payout Request or stand alone: an Organization that asked is settled by fulfilling its request, and one that asked through some other conversation is settled by recording the Payout directly. Neither route changes what a Payout is.
Exists only where money actually arrived. A transfer an operator has submitted but the bank has not yet confirmed is recorded on the Payout Request, which is processing, and becomes a Payout when it lands — never before, because a transfer the bank later rejects must leave nothing behind.
_Avoid_: Withdrawal, transfer, disbursement, settlement run

**Payout Request**:
An Org Admin's ask to be paid a stated amount, which a Platform Operator answers by transferring the money off-platform and recording the Payout. An ask, not money: it moves nothing, and no figure in the system counts it.
Bounded by the Payable Balance when it is made, and only then — the balance moves afterwards, and the operator at the bank decides what to do about that.
Carries a snapshot of the Payout Profile as it stood when the request was made, so the account an operator was told to pay is readable forever, whatever the Organization's details became later.
A request is outstanding while it still awaits an answer — whether nobody has answered it yet, or an operator has submitted the transfer and the bank has not confirmed it. An Organization may have only one outstanding at a time.
It is processing over that second stretch: the organizer's money has been sent but has not arrived, and until the bank confirms it nobody may say it was paid. Deliberately not called _in flight_, which this project reserves for a Sale Reversal waiting on an API the platform polls; a processing Payout Request waits on a person going to look.
It ends paid, failed when the bank rejected the transfer, declined with a reason its asker can read, or cancelled by the Organization; all four are final, and a fresh ask is a fresh request. A failed request moved no money and leaves no Payout — the Organization corrects whatever the bank objected to and asks again, because the bank details a request carries are a snapshot and cannot be edited.
Only a pending request can be cancelled or declined: once the transfer has been submitted, neither party may take back an ask the bank is already acting on.
_Avoid_: Withdrawal request, payout order, disbursement request, cash-out
_Avoid for `processing`_: In flight, in progress, approved, awaiting settlement

**Payout Profile**:
Where an Organization is paid: the bank, the account and its type, the name on it, and the Organization's own Tax ID for the factura. Set by an Org Admin, read by Platform Operators, and by nobody else — it never reaches an Event's staff or any Customer-facing surface.
One per Organization, current rather than historical: it says where to pay today. What a given Payout Request was told to pay is the snapshot on that request.
Its Tax ID identifies the Organization being paid, not a buyer — the same two words as a Customer's Tax ID, about a different person entirely, and never a passport, because the beneficiary holds an Ecuadorian bank account.
_Avoid_: Bank details, payment method, payout account, billing info

**Withdrawable Balance**:
What the platform owes an Organization: the sum of Net Proceeds across its active Online Sales, minus all recorded Payouts. Signed, not clamped — a sale reversed after it was paid out leaves the Organization owing the platform, and the figure says so. An Organization-level figure; each Event separately shows its own accumulated Net Proceeds, which answers "what has this Event sent through the platform" rather than "what has the platform yet to hand over". What an Event earned across every Sales Channel is its Takings, which is a larger figure wherever the Event sold anywhere but online.
What is owed, not what can be asked for today: money the platform holds but has not yet cleared is owed all the same. The Payable Balance is the part that can be asked for.
_Avoid_: Available funds, wallet, account balance

**Payable Balance**:
The part of the Withdrawable Balance an Organization may ask for now: the same figure counting only sales that have cleared — recorded before today in Ecuador, with no Reversal Request still open on them.
Cleared sales are a subset of all sales, so it never exceeds the Withdrawable Balance, and the gap between the two is simply money that has not settled yet.
Waiting for the day to turn is what makes a sale safe to hand over: the Reversal Window shuts at 20:00 at the latest, so a sale from a previous day can no longer be undone by its buyer. It defends against the Customer, not against the platform — an Operator Reversal can undo any sale at any time, and one arriving after a settlement drives the Withdrawable Balance negative.
_Avoid_: Available balance, cleared balance, settled funds, ready-to-withdraw

**Affiliate Link**:
A named, trackable link to an Event's page that an Org Admin or Event Owner creates to attribute Online Sales to whoever is promoting the Event — a person, a channel, or a campaign ("María's Instagram", "Radio spot").
The link itself is the whole concept: there is no affiliate person entity, no commission, and no payout — the stats are informational only.
Belongs to one Event. Carries an editable display name and an immutable system-generated code; can be deactivated (it stops attributing but keeps its history) and reactivated, and deleted only while it has attributed nothing.
Shows its clicks, attributed active-sale count, and attributed Net Proceeds — a reversed sale drops out of the figures like it does everywhere else.
A dead or deactivated code in a URL never gets in the buyer's way: the Event page renders normally and nothing is counted.
Points the opposite way to a Registration Link: an Affiliate Link points *at* the Event page and attributes the sales that follow, while a Registration Link points *away* from it and attributes nothing.
_Avoid_: Referral link, promo link, tracking link, UTM, campaign, affiliate (as a person)

**Affiliate Attribution**:
The tie between an Online Sale and the Affiliate Link that drove it, decided last-click within the Attribution Window: landing on the Event page through a live Affiliate Link remembers that link for that buyer, the newest click wins, and the checkout that follows records the remembered link on the sale.
Only Online Sales are ever attributed — in-person and imported sales never travel through a link. A sale with no remembered link is simply unattributed.
_Avoid_: Conversion tracking, referral credit, source tagging

**Attribution Window**:
How long a buyer's click on an Affiliate Link is remembered for Affiliate Attribution: 7 days, per Event, newest click winning.
A platform rule chosen for display-only stats — generous enough to credit "clicked on the bus, bought at home", accepted cost that the buyer might have returned anyway.
_Avoid_: Cookie lifetime, lookback window, click validity period

**External Platform**:
A third-party ticketing service through which tickets may be sold outside this system.
Distinct from an Integration Partner, which manages this system programmatically rather than supplying sales to import.
Distinct too from External Registration: an External Platform is about sales that happened elsewhere and came back through a Sale Import, whereas External Registration is about a buyer who left and never came back — the site behind a Registration Link is deliberately not an External Platform, since the system stores a URL rather than a vendor.
_Avoid_: Partner platform, external vendor

**Sales Source**:
Where an imported Ticket Sale originated: `external_platform` (a third-party service such as Eventbrite) or `direct` (the Organization's own off-platform sale). Qualifies a Ticket Sale on the `import` Sales Channel.
_Avoid_: Origin, provider, vendor

**Direct Sale**:
A Ticket Sale the Organization made off-platform on its own — for example a cash or bank-transfer sale arranged directly with a Customer — recorded through a Sale Import with Sales Source `direct` and a Payment Method. Distinct from an In-Person Sale (live at a physical POS) and from an External Platform sale (made in a third-party system).
_Avoid_: Manual sale, offline sale, cash sale

**Sale Import**:
A batch upload of Ticket Sales, performed by a Member who can manage the Event's sales and counted against Ticket Type capacity. Each batch carries a Sales Source: `external_platform` sales made in a third-party service, or `direct` sales the Organization made itself. Recorded as a batch so the most recent import to an Event can be reversed. At launch the `direct` source ships first.
_Avoid_: Manual sale entry, external sale upload

**Sales list**:
The staff surface for exploring an Event's individual Ticket Sales — one row per Ticket Sale, filterable, sortable, and paginated. Visible to every Member of the Event (Org Admins, Event Owners, and Event Staff), unlike the owner-only Sale Import tool it shares a page with. It defaults to active sales and carries the Event's count of reversed ones, so a Sale Reversal is stated rather than a row that silently left the view; the count is the whole Event's, unmoved by the filters, and the reversed sales themselves are reached through the status filter. Distinct from the Import history, which lists Sale Import batches rather than individual sales, and from a Storefront listing, which lists Events to the public rather than sales to staff.
_Avoid_: Sales ledger, orders list, transactions table, report

**Sales Export**:
A spreadsheet of an Event's Ticket Sales — one row per Ticket Sale — downloaded from the Sales list and reflecting exactly the filters that were on screen. Carries each sale's Confirmation reference, its buyer including the Tax ID the sale was transacted under, and its transaction facts, as real typed cells so the recipient can sort, sum and pivot without recomputing anything. States each sale's Net Proceeds — read off the fee snapshots the sale froze, so a later Fee Handling change never rewrites what an old sale earned — and never itemises the Platform Fee or Fee IVA (ADR 0032). Breaks the sale's Ticket Sale Lines out into one numeric column per Ticket Type — the Event's whole catalog in display order, headed with each Ticket Type's current name and stable under the filters — plus a total quantity, so the file stays one row per Ticket Sale and summing its amount cannot count a multi-type sale twice. Money and quantities that do not apply to a row are blank rather than zero: a zero would be summed. States a Sale Reversal on the row rather than dropping it: a reversed sale keeps its Confirmation reference and its buyer, and carries when it was reversed and by which route — `customer`, `platform` or `import_undo`, the route only, never the Platform Operator behind an Operator Reversal or their note (ADR 0019). Both are blank on an active sale, and the file's default omits reversed sales entirely, because the status filter it inherits from the Sales list defaults to active. Opens on an Info sheet that explains the file to somebody who did not download it: the Event, the moment it was generated — which is also the moment its Ticket Type headings were read — the timezone named as the Event's, the row count, the currency, and the filters in words rather than as query parameters, stating outright that reversed sales were excluded when they were, and stating that a search was applied without ever writing down the term. A separate sheet rather than a preamble above the header row, because rows above a header break select-all, autofilter and pivot source ranges. Generated on the spot rather than queued, so it is capped at the same number of rows a Sale Import accepts — the system has one answer to how many sale rows travel in a file, and an export can never exceed what the importer would take back. Filters matching more than that are refused with a message naming how many sales matched, because the filters are the lever and the person needs to know how much narrower to go. Every generated file leaves one log line naming who took it, from which Organization and Event, under which structural filters, and how many rows — the search term never among them, since it matches customer email and Tax ID number and a log aggregator is a wider audience than the database. Restricted to Org Admins and Event Owners, unlike the Sales list it is taken from: it is the largest concentration of buyer data the platform emits, in a form that gets forwarded and kept. Distinct from the Sales list, which is a screen rather than a file; from the Sale Import file, which is an artifact returning to the platform rather than leaving it — the two are deliberately shaped so an export cannot be uploaded as an import; and from the Import history, which lists Sale Import batches rather than sales.
_Avoid_: Sales report, sales dump, data export, CSV, "the download" or "the spreadsheet" as names for the artifact. Downloading is still the name of the act — a button says Download, because that is what the reader is about to do — but the thing that arrives is a Sales Export.

## Customer identity

**Verified Customer**:
A Customer who has proven ownership of their email, by One-time Passcode or by Google Sign-In.
Unverified Customers exist as records — created by a Ticket Sale made on their behalf — but receive no email beyond Sale Confirmations and cannot sign in until they verify.
_Avoid_: Registered customer, confirmed customer, active customer

**Customer Session**:
A server-side Customer sign-in record on the Storefront, tied to a verified email.
Spans every Ticket Sale that Customer owns, across all Organizations.
Distinct from a Staff Session, with which it shares nothing but its Proof of Email Ownership methods.
_Avoid_: Session, login, token, cookie

**Confirmation Link**:
A signed link carried in a Sale Confirmation, granting access to that one Ticket Sale without signing in.
Distinct from a Customer Session, which spans every Ticket Sale the Customer owns, and from a Consent Confirmation Link, which may travel in the same email and confirms an optional consent rather than opening a sale.
_Avoid_: Magic link, access token, deep link

**Customer Area**:
The signed-in Storefront surface where a Customer sees their upcoming events, past Ticket Sales, what they Follow, and preferences.
Distinct from a Storefront listing, which shows Events to the anonymous public.
_Avoid_: Wallet, my tickets, account page, dashboard

**Avatar**:
A Customer's optional profile image, shown wherever the signed-in Customer is represented in the Storefront.
Set by upload, or seeded from Google Sign-In when the Customer has none; removable by the Customer, falling back to their initials.
Distinct from an Organization's Logo, which represents the Organization rather than a person.
_Avoid_: Profile picture, photo, user image, logo

**Tax ID**:
The identification a Customer supplies for tax declarations: a Tax ID Type and its number.
The Customer holds their one current Tax ID — editable and clearable by the person, and not an identity: Customers are identified by email, and the same Tax ID may appear on several Customers. Each Ticket Sale immutably records the Tax ID it was transacted under, which may differ from the Customer's stored one.
Required to record a Ticket Sale on the native Sales Channels (`online`, `in_person`); optional on `import`, where the sale happened elsewhere and the ID may never have been collected. A fact of the sale's buyer, never of an individual attendee.
An Organization's own identification on its Payout Profile is a Tax ID too, validated the same way. It answers a different question — who the platform is paying and invoicing — and the two never mix.
_Avoid_: ID number, identification, cédula (as the generic term), document number, national ID

**Phone Number**:
A Customer's telephone number, stored as one canonical international string and collected only to prefill the Payment Provider's hosted payment page.
Always optional and never invented: a buyer who gives none simply enters it on the payment page instead. Collected through online checkout and the Customer Area only, never on the staff-recorded sale form or in the Sale Import file, so it is sparsely populated and is not a way to reach every Customer.
_Avoid_: Mobile, cell, contact number, telephone, celular — as names for the concept. "Mobile" still names a *line type* where the rule turns on it, as the Ecuadorian validation message does: only a mobile is accepted there, and saying so is the whole point of the message.

**Tax ID Type**:
Which kind of identification a Tax ID is: `cedula` (Ecuadorian national identity card), `ruc` (Ecuadorian taxpayer registration, held by persons or companies), or `passport` (buyers without either). Determines how strictly the number is validated.
_Avoid_: Document type, ID class, tipo de identificación

## Consent

**Privacy Policy**:
The platform's full statement of what personal data it processes and why, published on its own Storefront page in every Locale, together with the Short Notice shown at capture moments.
What a Customer accepts when they accept — always a specific Policy Version, never "the policy" in the abstract.
_Avoid_: Terms, T&C, legal page, data policy

**Short Notice**:
The condensed privacy notice (capa 1) shown inline wherever consent is captured, linking to the full Privacy Policy.
Part of the Policy Version it belongs to: notice and policy change together or not at all.
_Avoid_: Banner, disclaimer, fine print, capa 1 (in code and prose)

**Policy Version**:
One published edition of the Privacy Policy and its Short Notice, preserved exactly as shown so that what a person accepted is readable forever.
Publishing a new one makes every Customer unaccepted again: Policy Acceptance is of a version, not of the policy's idea.
_Avoid_: Revision, policy update, version number (as the whole concept)

**Policy Acceptance**:
A Customer's affirmative acceptance of the current Policy Version, required before a Customer Session is established or an Online Sale is completed — the two acts where the person is at the keyboard.
Never collected by staff on a buyer's behalf: a box-office or imported Customer carries no acceptance until they first act on a Storefront surface themselves.
_Avoid_: Agreeing to terms, signup consent, mandatory consent

**Marketing Consent**:
A Customer's opt-in to marketing email — promotions, campaigns, partner content, and the Follow Digest, which it subsumes: the Digest's switch and this consent are one thing, granted and declined together (ADR 0034).
Optional, never pre-ticked, and never blocking: declining it costs no purchase and no session. Gates no transactional mail — Sale Confirmations and One-time Passcodes arrive regardless.
_Avoid_: Newsletter opt-in, email preferences, subscription, mercadotecnia

**Networking Consent**:
A Customer's authorization to have their profile data shown, through the complementary networking application, to other attendees of the same event and to its organizers.
This platform holds the authoritative state; the networking application reads it and keeps no truth of its own.
_Avoid_: Public profile, profile visibility, data sharing (unqualified)

**Consent Record**:
The append-only evidence of one capture act: who answered which boxes, when, on which channel, under which Policy Version, with technical proof of the circumstances.
Never edited and never deleted — the log records what happened; the Customer's current consent state, kept beside it, records what is true now. A tick that changed no state still leaves its record.
_Avoid_: Consent log, audit trail, consent table

**Pending Confirmation**:
The state of an optional consent ticked by someone who had not proven the email they typed: denied for sending, unanswered for prompting (ADR 0035).
Resolved when the proven owner answers at a later capture moment or presses the Consent Confirmation Link in their Sale Confirmation; unresolved, it sends nothing forever and never expires.
_Avoid_: Unconfirmed opt-in, double opt-in (as the state's name), limbo

**Consent Confirmation Link**:
The signed link carried in a Sale Confirmation that resolves a Pending Confirmation, pressing it being itself the proof of the address that the checkout lacked.
Named apart from the Confirmation Link, which grants access to one Ticket Sale: the two are different tokens with different purposes and neither opens what the other does. It confirms only the boxes the mail it travelled in offered, and only where they are still pending — a decision the owner has made since always outranks it.
_Avoid_: Confirmation Link (unqualified), opt-in link, verification link, double opt-in link

**Consent Withdrawal**:
A capture act that takes an optional consent back, moving it to denied — one word for the concept in code, schema, tests and English prose.
Not a new mechanism and not a new state: it is an ordinary capture whose answers are No, written through the same single consent-write path, leaving the same Consent Record with the same technical proof. What makes it identifiable is the record's prior state — the same row says both what was answered and what it replaced, so `granted → denied` is legible without reading the log in order.
Never applies to Policy Acceptance, which rests on a basis other than consent and whose withdrawal would re-gate the person rather than free them. Costs nothing: Tickets, the account, future purchases and all transactional mail are untouched, and anything withdrawn can be granted again.
_Avoid_: Revocation, revoke, revocatoria (in code), opt-out (as the concept), consent deletion, unsubscribe (unless it is the Marketing link)

**Withdraw All**:
The single act withdrawing every optional consent at once, leaving one Consent Record that says the Customer asked for everything rather than two that say they moved two controls.
Not deletion and not account closure — a Customer who withdraws everything keeps their Tickets, their account and their receipts. Offered only where the person has been told what it does and does not mean.
_Avoid_: Delete my data, close account, opt out of everything, unsubscribe all, erasure

**Passive**:
A description of a Customer whose optional consents all stand denied: no consent-based processing, contract-based processing untouched.
Always scoped to consent. Data is still held and still processed for the Tickets the person holds and for legal and security obligations, so copy promising that processing has stopped would be untrue.
A description and never a stored flag: it is read off the consent states, because a column recording it would be a second place for the same fact to be kept and a first place for it to disagree.
_Avoid_: Inactive, dormant, opted out, anonymized, deleted, suppressed

## Following

**Follow**:
A Customer's standing subscription to a Tag or an Organization, entitling them to hear about the Events that fall under it.
An act of subscription, not of shortlisting: its whole payload is the Follow Digest, and pressing it is a request to be written to. Any Tag may be Followed, Preset or Custom, and any Organization; a Follow belongs to the Customer rather than to a browser.
Held only by a Customer whose session carries Proof of Email Ownership — never by the sale-scoped session a Confirmation Link mints, which says only that somebody opened a receipt, and may be somebody it was forwarded to. Subscribing an address is a claim on it, so it takes the same proof signing in does.
Distinct from Discoverable, which is an Organization deciding whether an Event advertises itself; a Follow is a Customer deciding what they want brought to them.
_Avoid_: Favorite, bookmark, save, star, watch, subscribe (as the name of the act)

**Follow Digest**:
The one weekly email carrying everything a Customer Follows — never one email per Follow, and never one per Event.
Holds Events under two headings: those New to You, and those already shown that begin within the week, the latter marking any the Customer already holds Tickets to. Capped, so that Following busily yields a readable email rather than a catalogue; what does not fit is carried into a later Digest rather than dropped, and each heading points at the Storefront listing that holds the rest.
The only mail a Customer can turn off. Distinct from a Sale Confirmation or a One-time Passcode, which answer something the Customer just did and are never silenced.
_Avoid_: Newsletter, weekly email, notification, alert, roundup, campaign

**New to You**:
The test deciding whether an Event is news to a particular Customer: it has never appeared in a Follow Digest sent to them.
A fact about the reader, not about the Event. An Event published months ago is New to You the first time it reaches you — so Following something busy is answered at once rather than with silence until the next Event is announced — and an Event already shown to you never becomes new again.
_Avoid_: New, newly published, unseen, latest, recent, announced

**Unfollow**:
Removing one Follow, changing what the Follow Digest is about.
Distinct from Unsubscribing, which leaves every Follow standing and changes only whether the Digest is sent.
_Avoid_: Unsave, remove favorite, mute

**Unsubscribe**:
The Marketing-specific, link-driven special case of a Consent Withdrawal: turning a Customer's Follow Digest off while leaving every Follow intact — which, the Digest and Marketing Consent being one switch (ADR 0034), is declining Marketing Consent.
A Consent Withdrawal in every respect that matters, and named separately only because the surface is: it is the one reachable from a footer by anybody holding the email, without signing in. It writes the same Consent Record through the same write path, records what it took away, and is confirmed the same way. It withdraws Marketing Consent alone — never Networking Consent, which is what distinguishes it from Withdraw All.
Reversible from the Customer Area, and never a way to lose a list: a Customer who wants quiet keeps what they Followed. Touches no transactional mail — Sale Confirmations and One-time Passcodes arrive either way.
_Avoid_: Opt out, mute, disable notifications, delete follows, revoke

**Mail Locale**:
The language mail to a Customer is written in, remembered from the Storefront they last signed in on.
Exists because a Locale is a property of a page's address and mail has no address to carry one. Stands behind the Sale Locale rather than beside it: consulted when the mail is about no sale, or about a sale that named no language.
Words the sentences of a message, not the names inside it: the Follow Digest is the one mail that names Tags, and it alone names Preset Tags in the Customer's language as a Storefront page would while leaving Custom Tags as coined.
Belongs to the recipient, not to the reader of a page: switching the Storefront's language does not change it, and only a completed sign-in writes it.
A Customer's, and a Customer's alone: mail to a Member or a Platform Operator is written in their Staff Locale (ADR 0041).
_Avoid_: Digest Locale, email language, preferred language, user locale

**Sale Locale**:
The language of the Storefront page a Ticket Sale was completed on.
Evidence of a language the buyer chose in the moment they bought, so it outranks what the Customer's Mail Locale remembers, and it governs every mail about that sale however long afterwards it is sent.
Absent on a sale no page produced — a box office sale or an import — which is a different thing from a sale made in English.
_Avoid_: Checkout language, buyer locale, sale language

**Suggested Follow**:
A Tag or an Organization a Customer does not Follow, offered to them as one they might.
Named for what accepting it produces: a Follow, made with the same act and carrying the same
entitlement as one made anywhere else. Offered only where a Customer's own Follows are — never on
the surfaces where Events are browsed and bought — and always with the interest that produced it
named, so it reads as reasoned rather than guessed at.
Distinct from Discoverable, which is an Organization advertising an Event to everyone; a Suggested
Follow is the platform answering one Customer from what they already told it.
_Avoid_: Recommendation, for you, discover, related, similar

**Activity**:
How much is happening under a Tag or an Organization: the discoverable upcoming Events that carry
the Tag, or that the Organization is running.
A measure of supply and never of audience — it counts Events, not the Customers who Follow. It
answers whether Following something would bring the Customer anything, which is the only promise a
Follow makes; a subject with nothing upcoming has nothing to say to a Follow Digest, however many
people Follow it. This platform holds no count of a subject's Followers and shows none.
_Avoid_: Popularity, trending, hot, top, most followed

**Co-occurrence**:
The relationship between two Tags carried by the same Event, and the ground of every Suggested
Follow.
A fact about the catalogue rather than about Customers: Tags are related because Events wear them
together, never because Customers Follow them together. Reaches Organizations through the same
fact, an Organization being related to a Tag when its upcoming Events carry it — which is what
relates the two followable kinds at all, since Tags are worn by Events and never by Organizations.
_Avoid_: Similarity, affinity, collaborative filtering, taste graph

## Signing in

**Proof of Email Ownership**:
A demonstration that the person at the keyboard controls a given email address.
It is the whole of signing in on either surface: nobody registers, and no other fact about a person is asked for. The sole fact established beyond email ownership is a seeded Avatar on Google Sign-In.
_Avoid_: Authentication, login, credential, identity verification

**One-time Passcode**:
A short code sent to an email address and redeemed on the surface that asked for it, serving as Proof of Email Ownership.
A passcode minted for one surface can never open a session on the other.
_Avoid_: One-time password, magic code, PIN, two-factor code

**Google Sign-In**:
Proof of Email Ownership obtained from Google rather than from a One-time Passcode, and equal to one in force.
Yields an ordinary Staff Session or Customer Session on the surface it was used from, and carries no authority to any other surface or Organization.
_Avoid_: SSO, single sign-on, social login, OAuth login, federated identity, linked account
