# Event Ticketing

A point-of-sale system for organizations to create events, define ticket types, and sell tickets online or in person, with programmatic access for Integration Partners.

## Organization & access

**Organization**:
A group such as a promoter, venue, or company that owns Events and has members.
_Avoid_: Account, workspace, team

**Member**:
A person belonging to an Organization who may hold a role such as Org Admin, Event Owner, or Event Staff.
_Avoid_: User, account, teammate

**Org Admin**:
A member of an Organization with full authority over all Events in that Organization.
For now, equivalent in scope to an Event Owner on any single Event.
_Avoid_: Organization owner, super admin

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
_Avoid_: Show, gig, occurrence

**Ticket Type**:
A purchasable ticket category belonging to an Event, with a name, description, price, and capacity.
_Avoid_: Ticket tier, ticket class, fare, SKU

## Sales

**Customer**:
A person who buys a ticket.
At launch, not modeled beyond their role in an Online Sale.
_Avoid_: Buyer, purchaser, account

**Ticket Sale**:
A completed transaction in which tickets of one or more Ticket Types are sold, such as a cart checkout.
_Avoid_: Order, purchase, transaction

**Ticket Sale Line**:
One Ticket Type and the quantity sold within a Ticket Sale.
_Avoid_: Line item, cart item

**Sales Channel**:
The medium through which a Ticket Sale occurs: online or in person.
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

**External Platform**:
A third-party ticketing service through which tickets may be sold outside this system.
Distinct from an Integration Partner, which manages this system programmatically rather than supplying sales to import.
_Avoid_: Partner platform, external vendor

**Sale Import**:
A batch upload of Ticket Sales from an External Platform during a given period, performed by Event Staff, counted against Ticket Type capacity.
_Avoid_: Manual sale entry, external sale upload
