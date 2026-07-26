# Event Ticketing

A point-of-sale system for organizations to create events, define ticket types, and sell tickets online or in person, with programmatic access for Integration Partners.

## Organization & access

**Organization**:
A group such as a promoter, venue, or company that owns Events and has members.
Has an optional Logo.
_Avoid_: Account, workspace, team

**Logo**:
An Organization's brand image, set by an Org Admin and shown wherever the Organization is represented to staff or Customers (e.g. the Storefront).
Distinct from an Event's cover image, which represents a single Event rather than the Organization.
_Avoid_: Icon, avatar, brand image

**Member**:
A person belonging to an Organization who may hold a role such as Org Admin, Event Owner, or Event Staff.
_Avoid_: User, account, teammate

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
_Avoid_: Organization owner, super admin

**Event assignment**:
A grant linking a Member to an Event with a role of Event Owner or Event Staff.
Required for a non-Org Admin Member to act on that Event; Org Admins do not need assignments.
_Avoid_: Permission, ACL, grant

**Event Owner**:
A member with full authority over a specific Event.
For now, equivalent in scope to an Org Admin within that Event.
_Avoid_: Organizer, host, creator

**Event Staff**:
A member granted access to manage an Event's catalog and sell tickets for that Event.
_Avoid_: Ticket Type Manager, collaborator, delegate

**Integration Partner**:
An external service that manages Events, Ticket Types, and sales on behalf of an Organization.
For now, equivalent in scope to an Org Admin across all of that Organization's Events.
Distinct from an External Platform, which is where sales happen outside this system before they are imported.
_Avoid_: API client, developer, third-party app

**Integration**:
An authorization granting an Integration Partner org-wide access to an Organization's Events, catalog, and sales.
_Avoid_: API key, connection, webhook

## Catalog

**Event**:
A scheduled occurrence belonging to an Organization, for which tickets are sold.
Has scheduling (start and optional end, timezone, venue), optional markdown description and cover image, and a lifecycle status.
_Avoid_: Show, gig, occurrence

**Event status**:
Where an Event is in its lifecycle: `draft` (being prepared), `published` (visible and sellable when sales exist), or `cancelled` (terminal; no longer active).
Governs whether an Event is reachable at all; distinct from Discoverable, which governs whether a reachable Event advertises itself in listings.
_Avoid_: State, visibility flag, active/inactive

**Discoverable**:
A per-Event flag controlling whether a `published` Event appears in public listings — the Organization page and the global explorer — or is reachable by direct link only.
Only a `published` Event can be Discoverable; `draft` and `cancelled` Events are never listed. A non-Discoverable published Event is still fully sellable to anyone who has its link.
Any Member of the Organization may set an Event's Discoverable flag; every other event edit remains an Org Admin action.
_Avoid_: Public/private, hidden, unlisted, featured

**Storefront listing**:
A public surface that lists Discoverable Events: the Organization page (`/{orgSlug}`, one Organization's Events) or the global explorer (`/`, Discoverable Events across all Organizations). Distinct from the Event page, which is the single-Event product and checkout surface.
_Avoid_: Catalog, index, feed, directory

**Ticket Type**:
A purchasable ticket category belonging to an Event, with a name, description, price, capacity, and display order.
_Avoid_: Ticket tier, ticket class, fare, SKU

**Tag**:
A discovery facet an Event can wear, describing what kind of Event it is (e.g. "Music", "Workshop", "Techno"). An Event may carry several Tags, and Tags live in one system-wide shared pool reused across all Organizations. Aids Storefront discovery. Distinct from Ticket Type, which is a purchasable category within a single Event.
_Avoid_: Category, genre, label, keyword

**Preset Tag**:
A curated Tag seeded by the system and always offered as a filter in the global explorer's tag chip bar. The curated tier of the shared Tag pool.
_Avoid_: Default tag, official tag, built-in category

**Custom Tag**:
A Tag coined by an Organization when no existing Tag fits. It joins the same shared pool as Preset Tags and aids discovery through search and on the Organization and Event pages, but is not offered as a filter chip in the global explorer unless later promoted to a Preset Tag.
_Avoid_: User tag, private tag, org tag

## Sales

**Customer**:
A person who buys a ticket.
Identified by a platform-global email — one Customer spans every Organization they have bought from.
A Customer record is created by any Ticket Sale on any Sales Channel, whether or not the person has ever signed in, or by a first sign-in for an email no sale has reached; every Ticket Sale belongs to one.
Holds what the person asserts about themselves (their current name, later their preferences), while each Ticket Sale keeps its own immutable record of what was transacted. First and last name are stored separately; where a single display name is needed they are joined as "First Last".
_Avoid_: Buyer, purchaser, account, user, full name

**Ticket Sale**:
A completed transaction in which tickets of one or more Ticket Types are sold, such as a cart checkout.
_Avoid_: Order, purchase, transaction

**Ticket Sale Line**:
One Ticket Type and the quantity sold within a Ticket Sale.
_Avoid_: Line item, cart item

**Sales Channel**:
The medium through which a Ticket Sale is recorded: `online`, `in_person`, or `import`. Distinct from Sales Source, which further qualifies where an imported sale originated.
_Avoid_: Sale type, payment method

**Storefront**:
The public-facing channel through which a Customer completes an Online Sale.
_Avoid_: Shop, web store, e-commerce site

**Online Sale**:
A Ticket Sale completed through the Storefront, recorded the moment its Payment is approved.
_Avoid_: Web sale, e-commerce sale

**In-Person Sale**:
A Ticket Sale completed at a physical point of sale by Event Staff.
_Avoid_: Door sale, box office sale, walk-up sale

**Payment Method**:
How a Ticket Sale was paid. Recorded for Direct Sales, where the value is `cash` or `transfer`, and for Online Sales, where the value names the Payment Provider that collected the money (`payphone` at launch). Modeled as an extensible set so further methods can be added later.
_Avoid_: Payment type, tender, channel

**Payment Provider**:
An external service that collects money from a Customer on the platform's behalf during an Online Sale — PayPhone at launch. The platform holds the single merchant account with each Payment Provider; Organizations are settled outside the system. Distinct from an External Platform, which sells tickets itself, and from an Integration Partner, which manages the system programmatically.
_Avoid_: Gateway, processor, PSP, vendor

**Payment**:
A Customer's attempt to pay for tickets through a Payment Provider. Begins `pending` when checkout starts and ends `approved` (the moment its Ticket Sale is recorded), `failed` (declined or cancelled), or `expired` (abandoned). A Ticket Sale exists only for an approved Payment; a Payment that never completes never becomes a sale and never appears in the Sales list.
_Avoid_: Transaction, charge, checkout session, payment intent, order

**Capacity Hold**:
The claim a pending Payment places on Ticket Type capacity so a Customer cannot pay for tickets that sold out while they were paying. Derived from the pending Payments themselves — a Payment younger than the hold window holds its quantities; an older one has lapsed. Released by the Payment ending in any state or by the window passing.
_Avoid_: Reservation, cart lock, inventory block

**Sale Confirmation**:
A per-Ticket-Sale receipt carrying a human-readable reference code (e.g. `TP-3F9K2`), emailed to the Customer whenever a Ticket Sale is recorded. Not a per-attendee admission ticket; the model leaves room to attach individual tickets later.
_Avoid_: Ticket, receipt, order confirmation

**Platform Fee**:
The platform's commission on a Ticket Sale — a percentage (10% at launch) of the ticket price the Organization set. Always charged to the Organization by withholding from its proceeds, never to the Customer; Fee Handling only decides whether the buyer price is raised to cover it. The tickets themselves are the Organization's sale, and their taxation stays outside the system.
_Avoid_: Commission, service charge, take rate

**Fee IVA**:
Ecuadorian value-added tax (15% at launch) levied on the Platform Fee, because the fee is the platform's taxable service. Not a tax on the ticket itself — ticket-level tax remains the Organization's off-platform concern.
_Avoid_: Tax, VAT on tickets, sales tax

**Fee Handling**:
A per-Event choice of who bears the Platform Fee and its Fee IVA: `pass_on` (the Customer pays them on top of the ticket price; the default) or `absorb` (the Organization pays them out of the ticket price, and the Customer sees exactly the price the Organization set). One switch — the fee and its IVA always travel together.
_Avoid_: Fee mode, pricing mode, pass-through flag

**Net Proceeds**:
What an Online Sale leaves for the Organization once the Platform Fee and Fee IVA are withheld: the amount collected minus both. Under `pass_on` Fee Handling this equals exactly the ticket price the Organization set. Only Online Sales produce Net Proceeds — money from other Sales Channels never passes through the platform.
_Avoid_: Net revenue, earnings, take-home

**Payout**:
A recorded settlement in which the platform transfers accumulated Net Proceeds to an Organization. Recorded per Organization — money is settled with the Organization, not with individual Events. Recording is a platform-operator action; Organizations do not yet request Payouts themselves.
_Avoid_: Withdrawal, transfer, disbursement, settlement run

**Withdrawable Balance**:
The money an Organization can currently be paid: the sum of Net Proceeds across its active Online Sales, minus all recorded Payouts. An Organization-level figure; each Event separately shows its own accumulated Net Proceeds, which answers "what has this Event earned" rather than "what can be withdrawn."
_Avoid_: Available funds, wallet, account balance

**External Platform**:
A third-party ticketing service through which tickets may be sold outside this system.
Distinct from an Integration Partner, which manages this system programmatically rather than supplying sales to import.
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
The staff surface for exploring an Event's individual Ticket Sales — one row per Ticket Sale, filterable, sortable, and paginated. Visible to every Member of the Event (Org Admins, Event Owners, and Event Staff), unlike the owner-only Sale Import tool it shares a page with. Distinct from the Import history, which lists Sale Import batches rather than individual sales, and from a Storefront listing, which lists Events to the public rather than sales to staff.
_Avoid_: Sales ledger, orders list, transactions table, report

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
Distinct from a Customer Session, which spans every Ticket Sale the Customer owns.
_Avoid_: Magic link, access token, deep link

**Customer Area**:
The signed-in Storefront surface where a Customer sees their upcoming events, past Ticket Sales, and preferences.
Distinct from a Storefront listing, which shows Events to the anonymous public.
_Avoid_: Wallet, my tickets, account page, dashboard

## Signing in

**Proof of Email Ownership**:
A demonstration that the person at the keyboard controls a given email address.
It is the whole of signing in on either surface: nobody registers, and no other fact about a person is asked for or established.
_Avoid_: Authentication, login, credential, identity verification

**One-time Passcode**:
A short code sent to an email address and redeemed on the surface that asked for it, serving as Proof of Email Ownership.
A passcode minted for one surface can never open a session on the other.
_Avoid_: One-time password, magic code, PIN, two-factor code

**Google Sign-In**:
Proof of Email Ownership obtained from Google rather than from a One-time Passcode, and equal to one in force.
Yields an ordinary Staff Session or Customer Session on the surface it was used from, and carries no authority to any other surface or Organization.
_Avoid_: SSO, single sign-on, social login, OAuth login, federated identity, linked account
