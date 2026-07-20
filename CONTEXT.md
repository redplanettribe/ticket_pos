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

**Session**:
A server-side staff sign-in record tied to an email address.
The session may or may not have an active Member selected.
Signing in proves email ownership; acting in an Organization requires an active Member on the session.
_Avoid_: Login, token, cookie

**Active Member**:
The Member record currently selected on a staff Session.
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
Identified by a required email and name on every Ticket Sale, so the platform can email them a Sale Confirmation. Not otherwise modeled as an account at launch.
_Avoid_: Buyer, purchaser, account

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
A Ticket Sale completed through the Storefront.
_Avoid_: Web sale, e-commerce sale

**In-Person Sale**:
A Ticket Sale completed at a physical point of sale by Event Staff.
_Avoid_: Door sale, box office sale, walk-up sale

**Payment Method**:
How a Ticket Sale was paid. Recorded for Direct Sales, where the value is `cash` or `transfer` at launch. Modeled as an extensible set so further methods can be added later.
_Avoid_: Payment type, tender, channel

**Sale Confirmation**:
A per-Ticket-Sale receipt carrying a human-readable reference code (e.g. `TP-3F9K2`), emailed to the Customer whenever a Ticket Sale is recorded. Not a per-attendee admission ticket; the model leaves room to attach individual tickets later.
_Avoid_: Ticket, receipt, order confirmation

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
