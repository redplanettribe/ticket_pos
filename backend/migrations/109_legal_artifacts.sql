-- The legal text moves into the database (#558, spec #556, map #540).
--
-- Until this migration the Privacy Policy and the Términos y Condiciones lived
-- as Markdown files compiled into the backend binary
-- (backend/internal/consent/{policy,terms}/artifacts/), and a Policy Version or
-- Terms Version row carried only the fingerprint of those files. That was the
-- right shape while an edition could only be published by a deploy (ADR 0036).
-- It stops being the right shape the moment an operator can write an edition
-- from the Legal Center: text that only a Go build can change cannot be edited
-- by a person, and a fingerprint taken over a file nobody can reach from the
-- product is evidence about a repository rather than about a page.
--
-- So the bytes move HERE, and nothing a reader can see moves at all. The public
-- endpoints keep their address, their shape and their response body; what
-- changes is where the service reads them from. This migration is the move, and
-- it is written so that the move cannot be silently lossy: it carries the text
-- as literals and then PROVES the copy against the fingerprint the version rows
-- already carry, refusing to apply if a single byte drifted.
--
-- TWO PARALLEL CHILD TABLES, not one generalised `legal_artifacts` with a
-- `document_kind`. A single table would need either a polymorphic
-- (document_kind, version_id) pair with no referential integrity, or two
-- nullable foreign keys — and a real FK is what makes "this text belongs to
-- that edition" a fact the database enforces rather than a convention a query
-- can forget. ADR 0066 already ruled against generalising the two version
-- tables for the same reason: the Policy and the Terms version independently,
-- an edition of one must never re-gate the other, and the cheapest way to keep
-- that true is for neither to be able to reach the other's rows.
--
-- THE TEXT IS CARRIED HERE AS SQL LITERALS, never read out of the binary's
-- embed.FS. A migration that read the embedded artifacts could only ever run
-- against a binary that still shipped them — so they could never be deleted,
-- which is the whole point of this ticket — and rebuilding a database from
-- migrations in 2028 would replay against whatever the text had become by then,
-- or fail outright. A migration is a historical record: it says what the text
-- WAS on the day it moved, and it keeps saying that forever.
--
-- FORWARD-ONLY, like every migration here. There is no down migration: dropping
-- these tables would delete the only copy of the text that acceptance evidence
-- points at.

-- One row per (Policy Version, Locale, artifact). An artifact is one piece of
-- text a reader can be shown under an edition: the policy body, the Short
-- Notice, one checkbox label.
CREATE TABLE policy_version_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The edition this text belongs to. ON DELETE RESTRICT, matching the rest of
    -- the consent chain: a published edition is never deleted, and the database
    -- says so rather than trusting that nobody will try.
    version_id UUID NOT NULL REFERENCES policy_versions (id) ON DELETE RESTRICT,
    -- The language this text is written in ('en', 'es'). TEXT and not an enum:
    -- the set of published languages is a property of each EDITION, read from
    -- these rows, so adding or dropping a language is a publication and not a
    -- schema change. Dropping one therefore starts 404ing that language with no
    -- deploy, which is the intended behaviour.
    locale TEXT NOT NULL,
    -- Which artifact this is, in the vocabulary the surfaces use:
    -- 'short-notice', 'label-policy-acceptance', 'label-marketing-consent',
    -- 'label-networking-consent', 'policy'. The service maps a slug to the
    -- field it serves; a slug it does not know is text nothing renders.
    slug TEXT NOT NULL,
    -- The position of this artifact in the fingerprint preimage.
    --
    -- THE LOAD-BEARING COLUMN. Until now the hash preimage's order was the order
    -- of the fields in a Go struct (policy.ContentHash). The moment an operator
    -- can add or remove an artifact from an edition, that order has to be DATA,
    -- or the fingerprint of an edition would depend on which binary computed it.
    ordinal INT NOT NULL,
    -- The text itself, markdown, ALREADY TRIMMED.
    --
    -- Trimmed in the row rather than at render time because TrimSpace is what
    -- the fingerprint has always been taken over: the bytes stored here are the
    -- bytes served and the bytes hashed, with no normalisation step in between
    -- that a future reader could forget. The CHECK enforces it, so a paste with
    -- a trailing newline cannot quietly change an edition's hash.
    body TEXT NOT NULL CHECK (body <> '' AND body = btrim(body)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One text per artifact per language per edition.
    UNIQUE (version_id, locale, slug),
    -- And one artifact per position: two rows sharing an ordinal would make the
    -- preimage order — and therefore the fingerprint — depend on the tie break.
    UNIQUE (version_id, locale, ordinal)
);

-- The read path fetches every artifact of one edition in one query, so this is
-- the index that matters. The two UNIQUE keys above already cover it, and no
-- other lookup exists: nothing ever asks for "every edition of this slug".

-- The Terms' parallel table. Same columns, same constraints, its own foreign
-- key — see the header for why this is not one table with a `document_kind`.
CREATE TABLE terms_version_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES terms_versions (id) ON DELETE RESTRICT,
    locale TEXT NOT NULL,
    -- 'label-terms-acceptance', 'terms'.
    slug TEXT NOT NULL,
    ordinal INT NOT NULL,
    body TEXT NOT NULL CHECK (body <> '' AND body = btrim(body)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (version_id, locale, slug),
    UNIQUE (version_id, locale, ordinal)
);

-- THE TEXT OF EVERY EDITION THIS PLATFORM HAS EVER PUBLISHED.
--
-- Three editions: the Privacy Policy's `0-placeholder` (migration 060) and `1`
-- (migration 066), and the Terms' `1` (migration 105). The placeholder is here
-- because eleven Customers' acceptances point at it, and an acceptance whose
-- text cannot be produced is not evidence of anything. Its prose is the
-- placeholder prose it always was — bracketed markers and all — and it is
-- carried verbatim rather than tidied, for the reason every one of these
-- migrations states: an edition is a record of what was shown.
--
-- Each row is attached by LOOKING UP ITS EDITION BY LABEL rather than by a
-- hardcoded UUID, because the version rows were inserted with
-- `gen_random_uuid()` and every database has different ids for them. A database
-- that does not hold an edition simply gets no rows for it; a database that
-- does gets exactly the text that edition was published under, which the block
-- at the end of this file then proves.

-- policy edition "0-placeholder": 10 artifacts, 5 per language.
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'short-notice', 1, $legal$**[DENOMINACIÓN SOCIAL]** (RUC **[RUC]**) processes your name, email address, Tax ID, phone number and profile photo in order to sell you tickets and to send you the mail your own actions produce — and, only if you tick the boxes below, to send you marketing email and to show your profile through the complementary networking application. You can withdraw either optional consent at any time, and exercise your access, rectification and deletion rights, by writing to **[correo PDP]**.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-policy-acceptance', 2, $legal$I have read and accept the Privacy Policy of **[DENOMINACIÓN SOCIAL]**, and I authorize the processing of my personal data for the purposes it describes.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-marketing-consent', 3, $legal$I authorize **[DENOMINACIÓN SOCIAL]** to send me marketing email — promotions, campaigns and partner content — including the weekly Follow Digest about the organizations and tags I follow. Optional, and I can withdraw it at any time.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-networking-consent', 4, $legal$I authorize **[DENOMINACIÓN SOCIAL]** to show my profile data, through its complementary networking application, to other attendees of the same event and to that event's organizers. Optional, and I can withdraw it at any time.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'policy', 5, $legal$> **PLACEHOLDER EDITION.** This text is not legal advice and has not been reviewed by counsel. Every bracketed placeholder below — `[DENOMINACIÓN SOCIAL]`, `[RUC]`, `[correo PDP]`, `[DIRECCIÓN]` — is filled in by the real legal drop before go-live, and only then is the first real Policy Version published.

## Who processes your data

The controller of the personal data described here is **[DENOMINACIÓN SOCIAL]**, RUC **[RUC]**, with its registered address at **[DIRECCIÓN]**, which operates this ticketing platform (the "platform").

Questions about this policy, and every request to exercise the rights listed below, go to the data protection contact at **[correo PDP]**.

## What data we process

- **Identity and contact data**: your name, your email address, your Tax ID (cédula or RUC), and your phone number.
- **Profile data**: the profile photo you choose to upload, and the language you read the platform in.
- **Transaction data**: the tickets you buy, the events they belong to, the amounts paid, and the record of any purchase you undo.
- **Consent evidence**: what you were shown, what you answered, when, on which surface, and the technical circumstances of that moment — your IP address, your browser's user agent, your session identifier, and the address of the page you answered on. We keep this because the law requires us to be able to prove what you authorized.
- **Technical data** strictly necessary to serve pages, keep you signed in, and keep the platform secure.

We do not ask you for, and do not want, data about your health, your beliefs, your political opinions, or anything else the law treats as sensitive.

## Why we process it, and on what basis

| Purpose | Basis |
| --- | --- |
| Selling you a ticket and delivering it | Performance of the contract you enter into when you buy |
| Mail your own actions produce — purchase confirmations, sign-in passcodes, refund notices | Performance of that same contract |
| Legal, accounting and tax obligations | Compliance with a legal obligation |
| Marketing email, including the weekly digest about what you follow | **Your consent**, given by ticking the marketing box, and withdrawable at any time |
| Showing your profile to other attendees of an event and to that event's organizers, through the complementary networking application | **Your consent**, given by ticking the networking box, and withdrawable at any time |

Accepting this Privacy Policy is required to open a session or to complete a purchase, because we cannot lawfully process your data without telling you how. The two optional consents are exactly that: declining either of them costs you nothing — no purchase, no session, and no mail your own actions produce.

## Who we share it with

- **The organizer of the event you bought a ticket to**, so that they can admit you and account for the sale.
- **Our payment provider**, which processes your payment. We never store your card details; we never receive them.
- **Our email provider**, which delivers the mail described above.
- **The complementary networking application**, and only if you granted the networking consent. This platform holds the authoritative record of that answer; the networking application reads it and keeps no truth of its own.
- **Public authorities**, when the law obliges us to.

We do not sell your personal data, and we do not hand it to anyone for their own marketing.

## Where your data lives, and for how long

Your data is stored on servers operated by **[PROVEEDOR DE ALOJAMIENTO]** in **[PAÍS/REGIÓN]**. Where a transfer leaves the country, we make it under the safeguards the law requires.

We keep transaction records for **[PLAZO CONTABLE]** as our accounting and tax obligations require. We keep the evidence of what you consented to for as long as that consent can still be questioned. Everything else we keep only while your account exists.

## Your rights

You may ask us, at any time and free of charge, to:

- tell you what data of yours we hold and how we came by it;
- correct data that is wrong or incomplete;
- delete your data, where no legal obligation makes us keep it;
- hand your data over in a portable form;
- stop or limit a particular use of it;
- **withdraw either optional consent**, which we will honour without asking you why and without any effect on your purchases.

Write to **[correo PDP]** and we will answer within **[PLAZO DE RESPUESTA]**. Marketing email additionally carries a one-click unsubscribe link in every message, and you can turn the digest off yourself from your account at any time. If you believe we have handled your data badly, you may complain to **[AUTORIDAD DE PROTECCIÓN DE DATOS]**.

## Children

The platform is not intended for people under **[EDAD MÍNIMA]**, and we do not knowingly process their data.

## Changes to this policy

Each published edition of this policy is a Policy Version, kept exactly as it was shown, so that what you accepted stays readable. When we publish a materially new edition we ask you to accept it again the next time you sign in or buy; your standing answers to the optional boxes are not disturbed by that. The edition you are reading, its effective date, and the fingerprint of its text are shown with it.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'short-notice', 1, $legal$**[DENOMINACIÓN SOCIAL]** (RUC **[RUC]**) trata su nombre, correo electrónico, identificación tributaria, teléfono y foto de perfil para venderle entradas y enviarle el correo que producen sus propias acciones — y, solo si marca las casillas de abajo, para enviarle correo de mercadeo y mostrar su perfil a través de la aplicación complementaria de networking. Puede revocar cualquiera de los consentimientos opcionales en cualquier momento, y ejercer sus derechos de acceso, rectificación y eliminación, escribiendo a **[correo PDP]**.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-policy-acceptance', 2, $legal$He leído y acepto la Política de Privacidad de **[DENOMINACIÓN SOCIAL]**, y autorizo el tratamiento de mis datos personales para las finalidades que describe.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-marketing-consent', 3, $legal$Autorizo a **[DENOMINACIÓN SOCIAL]** a enviarme correo de mercadeo — promociones, campañas y contenido de socios — incluido el resumen semanal (Follow Digest) sobre las organizaciones y etiquetas que sigo. Es opcional y puedo revocarlo en cualquier momento.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-networking-consent', 4, $legal$Autorizo a **[DENOMINACIÓN SOCIAL]** a mostrar mis datos de perfil, a través de su aplicación complementaria de networking, a otros asistentes del mismo evento y a los organizadores de ese evento. Es opcional y puedo revocarlo en cualquier momento.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'policy', 5, $legal$> **EDICIÓN PROVISIONAL.** Este texto no es asesoría legal y no ha sido revisado por un abogado. Cada marcador entre corchetes — `[DENOMINACIÓN SOCIAL]`, `[RUC]`, `[correo PDP]`, `[DIRECCIÓN]` — se completa con el texto legal definitivo antes de la salida a producción, y solo entonces se publica la primera Versión de la Política real.

## Quién trata sus datos

El responsable del tratamiento de los datos personales descritos aquí es **[DENOMINACIÓN SOCIAL]**, RUC **[RUC]**, con domicilio en **[DIRECCIÓN]**, que opera esta plataforma de venta de entradas (la "plataforma").

Las consultas sobre esta política, y toda solicitud para ejercer los derechos enumerados más abajo, se dirigen al contacto de protección de datos en **[correo PDP]**.

## Qué datos tratamos

- **Datos de identidad y contacto**: su nombre, su correo electrónico, su identificación tributaria (cédula o RUC) y su número de teléfono.
- **Datos de perfil**: la foto de perfil que decida subir y el idioma en el que lee la plataforma.
- **Datos de transacción**: las entradas que compra, los eventos a los que pertenecen, los importes pagados y el registro de cualquier compra que deshaga.
- **Evidencia de consentimiento**: qué se le mostró, qué respondió, cuándo, en qué superficie y las circunstancias técnicas de ese momento — su dirección IP, el agente de usuario de su navegador, el identificador de su sesión y la dirección de la página en la que respondió. Conservamos esto porque la ley nos exige poder demostrar qué autorizó usted.
- **Datos técnicos** estrictamente necesarios para servir las páginas, mantener su sesión iniciada y proteger la plataforma.

No le pedimos, ni queremos, datos sobre su salud, sus creencias, sus opiniones políticas ni ninguna otra categoría que la ley considere sensible.

## Para qué los tratamos, y con qué base

| Finalidad | Base |
| --- | --- |
| Venderle una entrada y entregársela | Ejecución del contrato que celebra al comprar |
| Correo que producen sus propias acciones — confirmaciones de compra, códigos de acceso, avisos de reverso | Ejecución de ese mismo contrato |
| Obligaciones legales, contables y tributarias | Cumplimiento de una obligación legal |
| Correo de mercadeo, incluido el resumen semanal de lo que sigue | **Su consentimiento**, otorgado al marcar la casilla de mercadeo, y revocable en cualquier momento |
| Mostrar su perfil a otros asistentes del mismo evento y a los organizadores de ese evento, a través de la aplicación complementaria de networking | **Su consentimiento**, otorgado al marcar la casilla de networking, y revocable en cualquier momento |

Aceptar esta Política de Privacidad es obligatorio para abrir una sesión o completar una compra, porque no podemos tratar sus datos lícitamente sin decirle cómo. Los dos consentimientos opcionales son exactamente eso: negar cualquiera de ellos no le cuesta nada — ni la compra, ni la sesión, ni el correo que producen sus propias acciones.

## Con quién los compartimos

- **El organizador del evento cuya entrada compró**, para que pueda darle acceso y llevar la contabilidad de la venta.
- **Nuestro proveedor de pagos**, que procesa su pago. Nunca almacenamos los datos de su tarjeta; nunca los recibimos.
- **Nuestro proveedor de correo**, que entrega los mensajes descritos arriba.
- **La aplicación complementaria de networking**, y solo si otorgó el consentimiento de networking. Esta plataforma conserva el registro autoritativo de esa respuesta; la aplicación de networking la lee y no guarda una verdad propia.
- **Las autoridades públicas**, cuando la ley nos obliga.

No vendemos sus datos personales y no los entregamos a nadie para su propio mercadeo.

## Dónde viven sus datos, y por cuánto tiempo

Sus datos se almacenan en servidores operados por **[PROVEEDOR DE ALOJAMIENTO]** en **[PAÍS/REGIÓN]**. Cuando una transferencia sale del país, la realizamos con las garantías que exige la ley.

Conservamos los registros de transacciones durante **[PLAZO CONTABLE]**, según exigen nuestras obligaciones contables y tributarias. Conservamos la evidencia de lo que usted consintió mientras ese consentimiento pueda ser cuestionado. Todo lo demás lo conservamos solo mientras exista su cuenta.

## Sus derechos

Puede pedirnos, en cualquier momento y de forma gratuita, que:

- le digamos qué datos suyos tenemos y cómo los obtuvimos;
- corrijamos datos erróneos o incompletos;
- eliminemos sus datos, cuando ninguna obligación legal nos obligue a conservarlos;
- le entreguemos sus datos en un formato portable;
- detengamos o limitemos un uso concreto de ellos;
- **revoquemos cualquiera de los consentimientos opcionales**, lo que haremos sin preguntarle por qué y sin efecto alguno sobre sus compras.

Escriba a **[correo PDP]** y le responderemos en **[PLAZO DE RESPUESTA]**. Además, todo correo de mercadeo lleva un enlace de baja de un solo clic, y usted puede desactivar el resumen semanal desde su cuenta cuando quiera. Si considera que hemos tratado mal sus datos, puede reclamar ante **[AUTORIDAD DE PROTECCIÓN DE DATOS]**.

## Menores de edad

La plataforma no está dirigida a personas menores de **[EDAD MÍNIMA]**, y no tratamos sus datos a sabiendas.

## Cambios en esta política

Cada edición publicada de esta política es una Versión de la Política, guardada exactamente como se mostró, para que lo que usted aceptó siga siendo legible. Cuando publicamos una edición materialmente nueva le pedimos que la acepte de nuevo la próxima vez que inicie sesión o compre; sus respuestas vigentes a las casillas opcionales no se ven alteradas por ello. La edición que está leyendo, su fecha de entrada en vigor y la huella de su texto se muestran junto a ella.$legal$ FROM policy_versions v WHERE v.label = '0-placeholder';

-- policy edition "1": 10 artifacts, 5 per language.
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'short-notice', 1, $legal$**Multiticketing**, operated by **FUNDACION REDPLANETTRIBE** (RUC **1793228468001**), processes your name, email address, Tax ID, phone number and profile photo in order to sell you tickets and to send you the mail your own actions produce — and, only if you tick the boxes below, to send you marketing email and to show your profile through the complementary networking application. You can withdraw either optional consent at any time, and exercise your access, rectification and deletion rights, by writing to **info@redplanettribe.org**.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-policy-acceptance', 2, $legal$I have read and accept the Privacy Policy of **Multiticketing**, and I authorize the processing of my personal data for the purposes it describes.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-marketing-consent', 3, $legal$I authorize **Multiticketing** to send me marketing email — promotions, campaigns and partner content — including the weekly Follow Digest about the organizations and tags I follow. Optional, and I can withdraw it at any time.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-networking-consent', 4, $legal$I authorize **Multiticketing** to show my profile data, through its complementary networking application, to other attendees of the same event and to that event's organizers. Optional, and I can withdraw it at any time.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'policy', 5, $legal$# PERSONAL DATA PROTECTION POLICY FOR CLIENTS AND USERS OF THE PLATFORM: MULTITICKETING

> **Courtesy translation.** The authoritative text of this policy is the Spanish one, published at the same address in Spanish. This English version is provided so that English-speaking readers can understand what they are accepting; in the event of any discrepancy between the two, the Spanish version prevails.

## 1. Purpose and Controller

### 1.1. Purpose

The "Personal Data Protection Policy for Clients and Users of the Multiticketing Platform" (hereinafter, the "POLICY") has as its purpose to explain how MULTITICKETING processes the personal data of the clients, users, participants and other data subjects who access, interact with or use the digital platform available on the website legal.multiticketing.com, as well as any other application, module, feature or digital channel enabled by MULTITICKETING (together, the "PLATFORM").

The POLICY covers the whole of the operations, interactions and records that take place on the PLATFORM, which may consist of the following activities: user registration, issuing and managing tickets for events, configuring user profiles, issuing invoices, notifications about event attendance, sending direct marketing communications, storing user information, networking features between participants, and any other activity proper to the business of MULTITICKETING carried out through the PLATFORM.

### 1.2. Controller

In accordance with the Organic Law on the Protection of Personal Data, its Regulations and other applicable rules on this matter (hereinafter, the "REGULATION"), MULTITICKETING acts as the "Controller", under the terms and conditions set out in the POLICY.

**1.2.1.** The contact information of MULTITICKETING is as follows:

- Company name: FUNDACION REDPLANETTRIBE.
- RUC (taxpayer registry number): 1793228468001.
- Registered address: ISLA MARCHENA N42-138, AVE GRANADOS, CONJUNTO PORTAL D ARAGON 1, QUITO.
- Telephone: 0983262407.
- Email address for communications about personal data protection: info@redplanettribe.org.

**1.2.2.** The contact information of the Data Protection Officer ("DPO") is as follows:

- Name: Pedro Díaz Cruz.
- Business address: Isla Marchena, Portal de Aragón 1.
- Email address for communications about personal data protection: info@redplanettribe.org.

## 2. Data subject

For the purposes of the POLICY, the data subject (hereinafter, the "SUBJECT" or the "SUBJECTS") means the natural person, of legal age, who belongs to one or more of the following categories:

**2.1.** Clients: the SUBJECTS who contract the services offered by MULTITICKETING through the PLATFORM, whether for organizing, managing or promoting events. This category includes the natural persons or representatives of legal entities who enter into a contract or service agreement with MULTITICKETING, as well as the strategic allies who use the PLATFORM to run their events.

**2.2.** Users: the SUBJECTS who access, register with or interact with the PLATFORM, whether as occasional or recurring visitors, in order to browse the event offering, confirm their attendance, buy tickets, configure their profile, take part in networking features or use any other digital resource made available by MULTITICKETING. This category comprises those who browse the website legal.multiticketing.com, complete electronic registration forms or access other digital resources of the PLATFORM, without necessarily having contracted an event organization service.

**2.3.** Participants: the SUBJECTS who register for one or more events published or managed through the PLATFORM, providing their personal data in order to confirm their attendance, receive their entry ticket and access the features associated with the event, such as networking between attendees. This category includes people who have provided their personal data in the context of a specific event, without that necessarily implying that they are Clients of MULTITICKETING.

**2.4.** Other subjects: any other type of SUBJECT who does not fall into one of the preceding categories and who accesses or interacts with the PLATFORM for various reasons, among them: (i) visiting or browsing the MULTITICKETING website without registering; (ii) having had a prior contractual relationship with MULTITICKETING; (iii) accessing events or services through strategic alliances of MULTITICKETING; or (iv) being legal entities represented by natural persons, or natural persons represented by attorneys-in-fact or agents operating on the PLATFORM.

## 3. Legal basis

The POLICY applies to the processing of the personal data of the SUBJECTS on the PLATFORM, collected or gathered through the various mechanisms enabled by MULTITICKETING. Accordingly, MULTITICKETING processes the personal data of the SUBJECTS exclusively in accordance with the provisions of the REGULATION. This includes the processing of data in the context of commercial relationships, performance of contractual obligations or pre-contractual measures, legal obligation, judicial provisions, legitimate interest, or where necessary with the explicit consent of the SUBJECTS.

## 4. Origin of the data

MULTITICKETING may access and/or collect the personal data of the SUBJECTS for the purposes set out in the POLICY in the following ways:

(i) Through the access, browsing, registration and/or use of the PLATFORM by the SUBJECTS.

(ii) Through direct and voluntary provision by the SUBJECTS. Such provision may be made through electronic registration forms, online acceptance of terms and conditions, and other mechanisms enabled by MULTITICKETING on the PLATFORM.

(iii) Through the internal generation of data derived from the SUBJECT's interaction with the PLATFORM, such as the issuing of tickets, attendance records and profile configurations.

(iv) Through provision by third parties who have sufficient authorization from the SUBJECTS, including strategic allies who contract the services of MULTITICKETING for the organization of events.

(v) Through collection from legitimate and authorized sources.

(vi) Through access by any other means provided for in the REGULATION.

In every case, MULTITICKETING will process the personal data of each SUBJECT — both data obtained before the issuing of this POLICY and data collected in the future — in accordance with the following parameters: (i) in conformity with the provisions of the REGULATION; (ii) subject to the contracts or terms and conditions entered into by MULTITICKETING with the SUBJECT; and (iii) subject to the privacy or personal data protection policies and notices of MULTITICKETING, including this POLICY.

## 5. Personal databases

Personal data will be stored in databases administered by MULTITICKETING. These databases are organized and managed in accordance with the principles established in the REGULATION, safeguarding the integrity, confidentiality and availability of the information they contain.

The information required by the REGULATION will be registered and kept up to date by MULTITICKETING with the National Registry of Personal Data Protection, administered by the Superintendency of Personal Data Protection, once the latter is operational. If the Registry is already in operation, the SUBJECTS may verify the registration of the corresponding database with that entity.

## 6. Types of personal data

The data processed by MULTITICKETING may fall into any of the following categories:

### 6.1. Non-sensitive data:

- Identification data: first names, surnames, national identity card or passport number, among others.
- Contact data: contact telephone number, email address, city, province, address, among others.
- Academic and professional data: profession, occupation, position, academic background, qualifications, work and/or professional experience, institution or company the subject belongs to, among others.
- Employment data: company or institution where the subject works, position, area or department, among others.
- Economic and financial data: billing information, payment data processed through the enabled payment gateways, among others.
- Image and/or voice data: profile photograph uploaded voluntarily by the SUBJECT on the PLATFORM for identification and networking purposes between event participants.

### 6.2. Sensitive data:

MULTITICKETING does not deliberately collect or process sensitive personal data of the SUBJECTS. In the event that, through exceptional circumstances or voluntary provision by the SUBJECT, data of this nature is received, MULTITICKETING will process it with the highest level of protection in accordance with the REGULATION.

MULTITICKETING undertakes to use the data exclusively for the purposes established in this document and in the electronic agreements entered into by the SUBJECT.

## 7. Retention period

The SUBJECT's information will be retained in accordance with the REGULATION and other applicable rules in force. Data collected through the PLATFORM will be retained for as long as the contractual or usage relationship linked to the declared purposes subsists and, once it has ended, for the additional periods required in compliance with legal obligations, judicial provisions or for the defence of the interests of MULTITICKETING. All of the above takes into account the limitation periods established in Ecuadorian law.

In particular, data linked to the issuing of invoices and to tax obligations will be retained for a minimum period of seven (7) years, in accordance with the tax rules in force. Audit records (logs), access logs and technical browsing data will be retained for the period necessary to comply with applicable legal obligations, to address security incidents and to provide evidentiary support for the transactions carried out on the PLATFORM.

Once the retention period has expired, the data will be deleted, anonymized or blocked, as appropriate, in accordance with the REGULATION.

## 8. Processing of personal data

MULTITICKETING may carry out various types of processing of the SUBJECT's personal data, which may include, but is not limited to, collecting, recording, organizing, structuring, storing, modifying, processing, communicating, consulting, using, combining, deleting and destroying data.

Personal data will be used for any of the following uses or purposes, which may involve the processing of the categories of data described in the POLICY, as well as access by third parties, under the terms of this POLICY.

## 9. Purposes of the processing and legitimizing bases

### 9.1. On the basis of the performance of a contract or pre-contractual measures, MULTITICKETING may:

**9.1.1.** Manage the SUBJECT's registration, enrolment and account creation on the PLATFORM, authenticate their identity and administer the access flow.

**9.1.2.** Issue the tickets corresponding to the events for which the SUBJECT has confirmed attendance or bought entry through the PLATFORM.

**9.1.3.** Configure and manage the SUBJECT's profile on the PLATFORM, including the choice between a public or private profile for networking purposes between event participants.

**9.1.4.** Generate and issue the invoices corresponding to the services contracted through the PLATFORM, in accordance with the tax rules in force.

**9.1.5.** Store the information provided by the SUBJECT during their registration and use of the PLATFORM in order to guarantee the continuity and correct provision of the service.

**9.1.6.** Perform the obligations arising from the contracts or service agreements entered into between MULTITICKETING and the Clients or strategic allies who organize events through the PLATFORM.

**9.1.7.** Contact the SUBJECT by email, notifications on the PLATFORM or other enabled channels in order to inform them about the status of their registrations, tickets, events or other aspects related to the contractual relationship.

### 9.2. On the basis of compliance with a legal obligation, MULTITICKETING may:

**9.2.1.** Comply with tax, regulatory and control obligations, including the issuing of sales receipts, declarations or other information required by the rules in force.

**9.2.2.** Respond to requirements of administrative or regulatory authorities, in accordance with applicable legislation.

**9.2.3.** Implement or comply with provisions established by the Superintendency of Personal Data Protection through resolutions, guidelines or requirements.

### 9.3. On the basis of compliance with judicial provisions, MULTITICKETING may:

**9.3.1.** Comply with reasoned orders, mandates or requirements of judicial authorities requesting personal information.

**9.3.2.** Retain the data in order to respond to judicial requirements during the legal limitation period.

**9.3.3.** Enforce judgments, resolutions or awards issued by the competent judicial authorities.

### 9.4. On the basis of consent, MULTITICKETING may:

**9.4.1.** Send the SUBJECT direct marketing communications, including notifications about MULTITICKETING's own events or those of its strategic allies, promotions, campaigns and advertising content related to the PLATFORM's offering.

**9.4.2.** Share the data of the SUBJECT's public profile with other participants registered for the same event, in order to enable the networking features available on the PLATFORM, where the SUBJECT has voluntarily opted for a public profile.

### 9.5. On the basis of legitimate interest, MULTITICKETING may:

**9.5.1.** Send notifications ahead of events to remind the SUBJECT of the date, time and other details relating to their confirmed attendance.

**9.5.2.** Share the SUBJECT's data with the strategic allies who run the events for which the SUBJECT has registered, for purposes strictly linked to the management and logistics of the event.

**9.5.3.** Use the information collected for programmes to improve the user experience and the quality of the services offered through the PLATFORM.

**9.5.4.** Produce internal statistics using anonymized or dissociated data, for the purposes of analysing use of the PLATFORM, participation trends in events and service improvement.

## 10. Effects of not having access to the SUBJECT's data or of their refusal to provide it

The SUBJECT declares that they understand that the data collected by MULTITICKETING on the PLATFORM is necessary for the correct provision of the contracted services, as well as for compliance with applicable legal obligations. Consequently, the timely and truthful provision of the data by the SUBJECT is an indispensable condition for MULTITICKETING to be able to meet their requirements and comply with the purposes described in this document. The SUBJECT guarantees and takes responsibility for the data provided being accurate, complete, precise, verifiable, clear and up to date.

Refusal to provide data, the provision of incomplete, erroneous or inaccurate information, or the impossibility of accessing it, may have the following effects as the case may be: (i) preventing the SUBJECT's registration on the PLATFORM; (ii) making it impossible to issue tickets or confirm attendance at events; (iii) preventing the generation of invoices and the processing of payments; or (iv) adversely affecting the quality and timeliness of the provision of the services.

Where the purpose of the processing is based on the SUBJECT's consent, refusal to provide the data or its withdrawal will not affect access to the essential services of the PLATFORM that rest on other legitimizing bases.

## 11. Third parties with access to data, transfers and recipients of personal data

### 11.1. Cases in which MULTITICKETING will allow third parties to access personal data

MULTITICKETING may allow access to or transfer the SUBJECT's personal data in accordance with this POLICY, with the contracts entered into with third parties and with the REGULATION. The SUBJECT understands and accepts that MULTITICKETING may share their personal information with natural or legal persons located in the Republic of Ecuador or abroad, in the following cases:

**11.1.1.** Where they act as processors of personal data, providing services on behalf of and under the documented instructions of MULTITICKETING, in order to fulfil the purposes detailed in this POLICY.

**11.1.2.** Where it is necessary to make a transfer or communication to a third party acting as a recipient of the data, in accordance with the REGULATION.

### 11.2. Categories of third parties that may access or receive the SUBJECT's personal data

The categories of third parties that may access or receive the SUBJECT's personal data are set out below:

**11.2.1. Strategic allies:**

- Event organizers who contract the services of MULTITICKETING for the management and running of events through the PLATFORM.
- Entities or companies involved in implementing campaigns, programmes and initiatives linked to the events managed by MULTITICKETING.

**11.2.2. Technology and operations service providers:**

- Providers of cloud hosting, data processing and storage services, intended for the technical operation of the PLATFORM. MULTITICKETING currently uses the services of Sevalla by Kinsta and Google Cloud.
- Providers of payment gateways for processing the financial transactions arising from the purchase of tickets and other services. MULTITICKETING currently uses the services of Payphone.
- Providers of notification services by email, messaging applications or other channels enabled for communication with the SUBJECTS.

### 11.3. Cases in which consent will not be required to disclose the data

MULTITICKETING undertakes not to disclose or share the SUBJECT's personal data without their prior consent. The following cases are, however, excepted:

- Requests for information from administrative or judicial authorities in the exercise of their functions.
- Requests from administrative authorities whose purpose is the subsequent processing of data for historical, statistical or scientific purposes, provided that such data is duly dissociated or at least anonymized.
- In general, requests or duties to provide information founded on legal provisions and contractual obligations entered into with the SUBJECT or on another applicable legitimizing basis, as the case may be.
- Where another legitimizing basis prescribed in the REGULATION applies.

## 12. International transfers of personal data

MULTITICKETING informs the SUBJECT that, by virtue of the use of technology service providers whose servers are located outside Ecuadorian territory, the SUBJECT's personal data may be subject to international transfer. In particular, the cloud hosting services provided by Sevalla by Kinsta and Google Cloud involve the storage and processing of data on servers located in the United States of America.

These international transfers will be carried out in conformity with the rules in force and with the provisions issued by the Superintendency of Personal Data Protection on international transfers of personal data, guaranteeing at all times an adequate level of protection of the SUBJECT's data.

MULTITICKETING has entered into the corresponding contractual instruments with its processors located abroad, in order to ensure compliance with the obligations established in the REGULATION.

## 13. Security measures

MULTITICKETING will implement the appropriate technical and organizational measures to guarantee a level of security appropriate to the risk, in accordance with the REGULATION. The purpose of those measures is to protect the SUBJECT's personal data against unauthorized access, use, modification, disclosure or destruction, as well as to guarantee the confidentiality, integrity and availability of the information.

The measures implemented include, without limitation, the following:

- Use of encryption protocols for the transmission of data through the PLATFORM.
- Logical access controls based on user roles and profiles.
- Storage of data on secure servers with periodic backups.
- Internal password and authentication management policies.
- Staff training on personal data protection.
- Confidentiality agreements entered into with staff and with processors.
- Periodic information security assessments.

MULTITICKETING will review and update the security measures implemented periodically, in order to align them with the technological standards in force and with the provisions of the REGULATION.

## 14. Automated decisions and profiling

MULTITICKETING informs the SUBJECT that the PLATFORM allows the configuration of user profiles, by means of which SUBJECTS may select a public or private profile for networking in accordance with their preferences. This feature is based on the information provided voluntarily by the SUBJECT and does not involve automated profiling with legal or significant effects for the SUBJECT.

MULTITICKETING does not currently carry out scoring, profiling or automated decision-making that produces legal effects on the SUBJECT or significantly affects them. In the event that such processes are implemented in the future, MULTITICKETING will inform the SUBJECT beforehand and will guarantee their right to obtain human intervention, to express their point of view and to contest the decision, in accordance with the REGULATION.

## 15. Cookies and tracking technologies

The PLATFORM may use cookies and other similar tracking technologies to improve the SUBJECT's browsing experience, facilitate the technical operation of the PLATFORM, collect statistical information about use of the website and personalize the content shown to the SUBJECT.

The SUBJECT may configure their browser to reject all or some cookies, or to receive a notice when a cookie is sent. However, if the SUBJECT disables or rejects cookies, some features of the PLATFORM may be unavailable or may not work correctly.

For further information about the use of cookies, the SUBJECT may consult the cookie policy published on the PLATFORM.

## 16. Rights of the data subject

The SUBJECT may exercise the rights over their personal data by sending a request to the email address info@redplanettribe.org.

### 16.1. Content of the Request

The Request must include at least the following:

**16.1.1.** The SUBJECT's name and email address or any other means of receiving responses.

**16.1.2.** A document evidencing the identity of the applicant and, where applicable, that of their representative together with the corresponding authorization.

**16.1.3.** A clear and precise description of the personal data in respect of which the SUBJECT seeks to exercise one of the rights established in the Organic Law on the Protection of Personal Data.

**16.1.4.** The specific request.

### 16.2. Rights that may be exercised by the SUBJECT

In relation to the processing of their personal data, the SUBJECTS may exercise the following rights:

- **Right of access:** the SUBJECT may request information about whether their personal data is being processed by MULTITICKETING, as well as obtain a copy of it.
- **Right of rectification:** the SUBJECT may request the correction or updating of their personal data where it is inaccurate, incomplete or out of date.
- **Right of erasure or deletion:** the SUBJECT may request the deletion of their personal data where, among other reasons, it is no longer necessary for the purposes for which it was collected, they have withdrawn their consent, or the data has been processed unlawfully.
- **Right to object:** the SUBJECT may object to the processing of their personal data where that processing is based on the legitimate interest of MULTITICKETING, or where the data is processed for direct marketing purposes.
- **Right to portability:** the SUBJECT may request the provision of their personal data in a compatible, up-to-date, structured, common, interoperable and machine-readable format.
- **Right to restriction of processing:** the SUBJECT may request that the processing of their personal data be suspended in the cases provided for in the REGULATION.

MULTITICKETING will respond to the SUBJECTS' requests within the periods and under the conditions established by the REGULATION. In the event that the request cannot be met in whole or in part, MULTITICKETING will inform the SUBJECT of the reasons for its refusal, which may be founded on the existence of legal obligations, prevailing legitimate interests, or any other cause established in the REGULATION.

## 17. Withdrawal of consent

The SUBJECT may withdraw the authorization and consent granted to MULTITICKETING for one or more of the purposes of the processing of their personal data, by submitting a written request sent to the email address info@redplanettribe.org.

## 18. Portability of personal data

Upon express written request by the SUBJECT, which must be sent to the email address info@redplanettribe.org, MULTITICKETING may provide their personal data in a compatible, up-to-date, structured, common, interoperable and machine-readable format, preserving its characteristics, so that it may be held by the SUBJECT themselves or handed over to another controller designated by the SUBJECT.

## 19. Applicable law

The terms contained in this POLICY are governed by and interpreted in accordance with the laws in force in the Republic of Ecuador.

## 20. Consent

The SUBJECT declares that they have been informed of, and accepts, without reservation or condition of any kind, the declarations and commitments contained herein. In particular, they expressly, freely, specifically, unequivocally and on an informed basis accept and approve the handling of their personal data by MULTITICKETING, in the manner and to the extent detailed in the POLICY.

## 21. Use of electronic means

The SUBJECT gives their consent to the use of electronic and technological means in their relationship with MULTITICKETING, and undertakes for that purpose to act in good faith and for legal and lawful ends. In that sense, the SUBJECT ratifies, accepts and irrevocably declares that the instructions and communications made to MULTITICKETING through electronic, computing and telematic means — such as the internet, email, electronic forms, and other mechanisms enabled by MULTITICKETING — are fully valid and binding.

In addition, the SUBJECT makes the following declarations: (i) They have sufficient legal capacity and are empowered to make the declarations contained in this clause and consequently to bind themselves to MULTITICKETING, and are therefore fully responsible for their scope and effects. (ii) The keys, codes and other security measures used as electronic validation methods have the legal effects established in the Electronic Commerce, Electronic Signatures and Data Messages Act, its Regulations and other applicable rules. (iii) They expressly acknowledge and accept that the POLICY will be binding and enforceable from its acceptance, which may be given through the electronic means enabled by MULTITICKETING.

## 22. Amendments

MULTITICKETING reserves the right to update, modify or delete this POLICY at any time, whether in whole or in part. Any modification will be published or notified to the SUBJECTS through the PLATFORM or other channels that MULTITICKETING enables for that purpose. Such amendments will take effect and their application will be mandatory from that notification or publication.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'short-notice', 1, $legal$**Multiticketing**, operada por **FUNDACION REDPLANETTRIBE** (RUC **1793228468001**), trata su nombre, correo electrónico, identificación tributaria, teléfono y foto de perfil para venderle entradas y enviarle el correo que producen sus propias acciones — y, solo si marca las casillas de abajo, para enviarle correo de mercadeo y mostrar su perfil a través de la aplicación complementaria de networking. Puede revocar cualquiera de los consentimientos opcionales en cualquier momento, y ejercer sus derechos de acceso, rectificación y eliminación, escribiendo a **info@redplanettribe.org**.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-policy-acceptance', 2, $legal$He leído y acepto la Política de Privacidad de **Multiticketing**, y autorizo el tratamiento de mis datos personales para las finalidades que describe.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-marketing-consent', 3, $legal$Autorizo a **Multiticketing** a enviarme correo de mercadeo — promociones, campañas y contenido de socios — incluido el resumen semanal (Follow Digest) sobre las organizaciones y etiquetas que sigo. Es opcional y puedo revocarlo en cualquier momento.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-networking-consent', 4, $legal$Autorizo a **Multiticketing** a mostrar mis datos de perfil, a través de su aplicación complementaria de networking, a otros asistentes del mismo evento y a los organizadores de ese evento. Es opcional y puedo revocarlo en cualquier momento.$legal$ FROM policy_versions v WHERE v.label = '1';
INSERT INTO policy_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'policy', 5, $legal$# POLÍTICA DE PROTECCIÓN DE DATOS PERSONALES PARA CLIENTES Y USUARIOS DE LA PLATAFORMA: MULTITICKETING

## 1. Objeto y Responsable del tratamiento.

### 1.1. Objeto.

La "Política de Protección de Datos Personales para Clientes y Usuarios de la Plataforma Multiticketing" (en adelante, la "POLÍTICA") tiene por objeto informar cómo (en adelante, "MULTITICKETING") trata los datos personales de los clientes, usuarios, participantes y demás titulares de datos que acceden, interactúan o utilizan la plataforma digital disponible en el sitio web legal.multiticketing.com, así como cualquier otra aplicación, módulo, funcionalidad o canal digital habilitado por MULTITICKETING (en conjunto, la "PLATAFORMA").

La POLÍTICA comprende la totalidad de operaciones, interacciones y registros que se producen en la PLATAFORMA, y que pueden consistir en las siguientes actividades: registro de usuarios, emisión y gestión de tickets para eventos, configuración de perfiles de usuario, emisión de facturas, notificaciones sobre asistencia a eventos, envío de comunicaciones de mercadotecnia directa, almacenamiento de información de usuarios, funcionalidades de networking entre participantes, y cualquier otra actividad propia del giro de MULTITICKETING que se efectúe a través de la PLATAFORMA.

### 1.2. Responsable del tratamiento.

De acuerdo con lo establecido en la Ley Orgánica de Protección de Datos Personales, su Reglamento y demás normativa aplicable en esta materia (en adelante, la "REGULACIÓN"), MULTITICKETING actúa como "Responsable del Tratamiento", bajo los términos y condiciones estipulados en la POLÍTICA.

**1.2.1.** La información de contacto de MULTITICKETING es la siguiente:

- Dnominación social: FUNDACION REDPLANETTRIBE.
- RUC: 1793228468001.
- Domicilio social: ISLA MARCHENA N42-138, AVE GRANADOS, CONJUNTO PORTAL D ARAGON 1, QUITO.
- Teléfono: 0983262407.
- Correo electrónico para comunicaciones sobre protección de datos personales: info@redplanettribe.org.

**1.2.2.** La información de contacto del Delegado de Protección de Datos (por sus siglas, "DPD") es la siguiente:

- Nombres y apellidos: Pedro Díaz Cruz.
- Domicilio laboral: Isla Marchena, Portal de Aragón 1.
- Correo electrónico para comunicaciones sobre protección de datos personales: info@redplanettribe.org.

## 2. Titular de los datos.

Para los fines de la POLÍTICA, se entenderá como titular de los datos (en adelante, el "TITULAR" o los "TITULARES") a la persona natural, mayor de edad que pertenezca a una o varias de las siguientes categorías:

**2.1.** Clientes: son los TITULARES que contratan los servicios ofrecidos por MULTITICKETING a través de la PLATAFORMA, ya sea para la organización, gestión o promoción de eventos. Esta categoría incluye a las personas naturales o representantes de personas jurídicas que suscriben un contrato o acuerdo de prestación de servicios con MULTITICKETING, así como a los aliados estratégicos que utilizan la PLATAFORMA para la ejecución de sus eventos.

**2.2.** Usuarios: son los TITULARES que acceden, se registran o interactúan con la PLATAFORMA, ya sea como visitantes ocasionales o recurrentes, para consultar la oferta de eventos, confirmar su asistencia, adquirir tickets, configurar su perfil, participar en funcionalidades de networking o utilizar cualquier otro recurso digital puesto a disposición por MULTITICKETING. Esta categoría comprende a quienes navegan en el sitio web legal.multiticketing.com, completan formularios electrónicos de registro o acceden a otros recursos digitales de la PLATAFORMA, sin que necesariamente hayan contratado un servicio de organización de eventos.

**2.3.** Participantes: son los TITULARES que se registran en uno o más eventos publicados o gestionados a través de la PLATAFORMA, proporcionando sus datos personales para confirmar su asistencia, recibir su ticket de ingreso y acceder a las funcionalidades asociadas al evento, tales como el networking entre asistentes. Esta categoría incluye a las personas que han proporcionado sus datos personales en el contexto de un evento específico, sin que aquello implique necesariamente que sean Clientes de MULTITICKETING.

**2.4.** Otros titulares: se refiere a cualquier otro tipo de TITULAR que no corresponde a alguna de las categorías precedentes y que accede o interactúa con la PLATAFORMA por diversos motivos, entre ellos: (i) visitar o navegar en el sitio web de MULTITICKETING sin registrarse; (ii) haber mantenido una relación contractual previa con MULTITICKETING; (iii) acceder a eventos o servicios a través de alianzas estratégicas de MULTITICKETING; o (iv) tratarse de personas jurídicas representadas por personas naturales, o de personas naturales representadas por apoderados o mandatarios que operen en la PLATAFORMA.

## 3. Base legal.

La POLÍTICA aplica al tratamiento de los datos personales de los TITULARES en la PLATAFORMA, recopilados o recabados a través de los distintos mecanismos habilitados por MULTITICKETING. Por tanto, MULTITICKETING trata exclusivamente los datos personales de los TITULARES conforme a las disposiciones de la REGULACIÓN. Esto incluye el tratamiento de datos en el contexto de relaciones comerciales, cumplimiento de obligaciones contractuales o medidas precontractuales, por obligación legal, disposiciones judiciales, por interés legítimo o cuando sea necesario con el consentimiento explícito de los TITULARES.

## 4. Origen de los datos.

MULTITICKETING podrá acceder y/o recoger los datos personales de los TITULARES para los fines previstos en la POLÍTICA de las siguientes formas:

(i) Por el acceso, navegación, registro y/o uso de los TITULARES en la PLATAFORMA.

(ii) Por la entrega directa y voluntaria por parte de los TITULARES. Esta entrega podrá ser conferida a través de formularios electrónicos de registro, aceptaciones en línea de términos y condiciones, y demás mecanismos habilitados por MULTITICKETING en la PLATAFORMA.

(iii) Por la generación interna de datos derivados de la interacción del TITULAR con la PLATAFORMA, tales como la emisión de tickets, registros de asistencia y configuraciones de perfil.

(iv) Por la entrega por parte de terceros que cuenten con la autorización suficiente de los TITULARES, incluyendo aliados estratégicos que contraten los servicios de MULTITICKETING para la organización de eventos.

(v) Por su recopilación de fuentes legítimas y autorizadas.

(vi) Por su acceso a través de cualquier otra forma prevista en la REGULACIÓN.

En todos los casos, MULTITICKETING tratará los datos personales de cada TITULAR, tanto aquellos obtenidos con anterioridad a la emisión de esta POLÍTICA, como los que se recopilen en el futuro, de acuerdo con los siguientes parámetros: (i) en conformidad con lo establecido en la REGULACIÓN; (ii) con sujeción a los contratos o términos y condiciones suscritos por MULTITICKETING con el TITULAR; y (iii) con sujeción a las políticas y avisos de privacidad o protección de datos personales de MULTITICKETING, incluida esta POLÍTICA.

## 5. Bases de datos personales.

Los datos personales se almacenarán en bases de datos administradas por MULTITICKETING. Estas bases de datos se encuentran organizadas y gestionadas conforme a los principios establecidos en la REGULACIÓN, garantizando la integridad, confidencialidad y disponibilidad de la información allí contenida.

La información requerida por la REGULACIÓN será registrada y actualizada por MULTITICKETING ante el Registro Nacional de Protección de Datos Personales, administrado por la Superintendencia de Protección de Datos Personales, una vez que este último se encuentre habilitado. Si el Registro ya estuviera en funcionamiento, los TITULARES podrán verificar la inscripción de la base de datos correspondiente ante la entidad últimamente mencionada.

## 6. Tipos de datos personales.

Los datos tratados por MULTITICKETING podrán corresponder a cualquiera de las siguientes categorías:

### 6.1. Datos no sensibles:

- Datos identificativos: nombres, apellidos, número de cédula de identidad o pasaporte, entre otros.
- Datos de contacto: teléfono de contacto, correo electrónico, ciudad, provincia, dirección, entre otros.
- Datos académicos y profesionales: profesión, ocupación, cargo, formación académica, titulaciones, experiencia laboral y/o profesional, institución o empresa a la que pertenece, entre otros.
- Datos laborales: empresa o institución donde trabaja, cargo, área o departamento, entre otros.
- Datos económicos y financieros: información de facturación, datos de pago procesados a través de pasarelas de pago habilitadas, entre otros.
- Datos de imagen y/o voz: fotografía de perfil cargada voluntariamente por el TITULAR en la PLATAFORMA para fines de identificación y networking entre participantes de eventos.

### 6.2. Datos sensibles:

MULTITICKETING no recopila ni trata de forma deliberada datos personales sensibles de los TITULARES. En caso de que, por circunstancias excepcionales o por entrega voluntaria del TITULAR, se reciban datos de esta naturaleza, MULTITICKETING los tratará con el más alto nivel de protección conforme a la REGULACIÓN.

MULTITICKETING se compromete a utilizar los datos exclusivamente para las finalidades establecidas en este documento y en los acuerdos electrónicos suscritos por el TITULAR.

## 7. Plazo de conservación.

La información del TITULAR será conservada de acuerdo con lo prescrito por la REGULACIÓN y la normativa vigente aplicable. Los datos recolectados a través de la PLATAFORMA serán conservados mientras subsista la relación contractual o de uso vinculada a las finalidades declaradas y, una vez concluida esta, durante los plazos adicionales exigidos en cumplimiento de obligaciones legales, disposiciones judiciales o para la defensa de los intereses de MULTITICKETING. Todo lo anterior tendrá en cuenta los plazos de prescripción establecidos en el ordenamiento jurídico ecuatoriano.

En particular, los datos vinculados a la emisión de facturas y obligaciones tributarias serán conservados por un plazo mínimo de siete (7) años, conforme a la normativa tributaria vigente. Los registros de auditoría (logs), bitácoras de acceso y datos técnicos de navegación serán conservados por el plazo necesario para cumplir con las obligaciones legales aplicables, atender incidentes de seguridad y respaldar probatoriamente las transacciones efectuadas en la PLATAFORMA.

Una vez vencido el plazo de conservación, se procederá a la supresión, anonimización o bloqueo de los datos, según corresponda, conforme a la REGULACIÓN.

## 8. Tratamiento de datos personales.

MULTITICKETING podrá realizar distintos tipos de tratamiento de los datos personales del TITULAR, pudiendo incluir, pero sin limitarse a, recopilar, registrar, organizar, estructurar, almacenar, modificar, procesar, comunicar, consultar, usar, combinar, suprimir y destruir datos.

Los datos personales serán destinados a cualquiera de los siguientes usos o fines, que podrán implicar el tratamiento de las categorías de datos descritas en la POLÍTICA, así como también el acceso por parte de terceros, según los términos de esta POLÍTICA.

## 9. Finalidades del tratamiento y bases legitimadoras.

### 9.1. Con base en la ejecución de un contrato o medidas precontractuales, MULTITICKETING podrá:

**9.1.1.** Gestionar el registro, inscripción y creación de cuenta del TITULAR en la PLATAFORMA, autenticar su identidad y administrar el flujo de acceso.

**9.1.2.** Emitir los tickets correspondientes a los eventos en los que el TITULAR haya confirmado su asistencia o adquirido su entrada a través de la PLATAFORMA.

**9.1.3.** Configurar y gestionar el perfil del TITULAR en la PLATAFORMA, incluyendo la selección entre perfil público o privado para fines de networking entre participantes de eventos.

**9.1.4.** Generar y emitir las facturas correspondientes por los servicios contratados a través de la PLATAFORMA, conforme a la normativa tributaria vigente.

**9.1.5.** Almacenar la información proporcionada por el TITULAR durante su registro y uso de la PLATAFORMA para garantizar la continuidad y correcta prestación del servicio.

**9.1.6.** Ejecutar las obligaciones derivadas de los contratos o acuerdos de prestación de servicios suscritos entre MULTITICKETING y los Clientes o aliados estratégicos que organizan eventos a través de la PLATAFORMA.

**9.1.7.** Contactar al TITULAR a través de correo electrónico, notificaciones en la PLATAFORMA u otros canales habilitados para informar sobre el estado de sus registros, tickets, eventos u otros aspectos relacionados con la relación contractual.

### 9.2. Con base en el cumplimiento de una obligación legal, MULTITICKETING podrá:

**9.2.1.** Cumplir con obligaciones tributarias, regulatorias y de control, incluyendo la emisión de comprobantes de venta, declaraciones u otra información exigida por la normativa vigente.

**9.2.2.** Atender requerimientos de autoridades administrativas o regulatorias, conforme a la legislación aplicable.

**9.2.3.** Implementar o cumplir con disposiciones establecidas por la Superintendencia de Protección de Datos Personales mediante resoluciones, guías o requerimientos.

### 9.3. Con base en el cumplimiento de disposiciones judiciales, MULTITICKETING podrá:

**9.3.1.** Cumplir órdenes, mandatos o requerimientos motivados de autoridades judiciales que soliciten información personal.

**9.3.2.** Conservar los datos para atender requerimientos judiciales durante el plazo legal de prescripción.

**9.3.3.** Ejecutar sentencias, resoluciones o laudos emitidos por autoridades judiciales competentes.

### 9.4. Con base en el consentimiento, MULTITICKETING podrá:

**9.4.1.** Remitir al TITULAR comunicaciones de mercadotecnia directa, incluyendo notificaciones sobre eventos propios de MULTITICKETING o de sus aliados estratégicos, promociones, campañas y contenido publicitario relacionado con la oferta de la PLATAFORMA.

**9.4.2.** Compartir los datos del perfil público del TITULAR con otros participantes registrados en un mismo evento, para facilitar las funcionalidades de networking habilitadas en la PLATAFORMA, cuando el TITULAR haya optado voluntariamente por un perfil público.

### 9.5. Con base en el interés legítimo, MULTITICKETING podrá:

**9.5.1.** Enviar notificaciones previas a los eventos para recordar al TITULAR la fecha, hora y demás detalles relativos a su asistencia confirmada.

**9.5.2.** Compartir los datos del TITULAR con los aliados estratégicos que ejecutan los eventos en los que el TITULAR se ha registrado, para fines estrictamente vinculados a la gestión y logística del evento.

**9.5.3.** Utilizar la información recopilada para programas de mejora de la experiencia del usuario y la calidad de los servicios ofrecidos a través de la PLATAFORMA.

**9.5.4.** Elaborar estadísticas internas mediante el uso de datos anonimizados o disociados, con fines de análisis del uso de la PLATAFORMA, tendencias de participación en eventos y mejora del servicio.

## 10. Efectos de la falta de acceso a los datos del TITULAR o de su negativa a entregarlos.

El TITULAR declara entender que los datos recolectados por MULTITICKETING en la PLATAFORMA son necesarios para la correcta prestación de los servicios contratados, así como para el cumplimiento de las obligaciones legales aplicables. En consecuencia, la entrega oportuna y veraz de los datos por parte del TITULAR es condición indispensable para que MULTITICKETING pueda atender sus requerimientos y cumplir con las finalidades descritas en este documento. El TITULAR garantiza y se responsabiliza de que los datos proporcionados sean exactos, íntegros, precisos, completos, comprobables, claros y actualizados.

La negativa a proporcionar datos, la entrega de información incompleta, errónea o inexacta, o la imposibilidad de acceder a los mismos, podrá tener los siguientes efectos según el caso: (i) impedir el registro del TITULAR en la PLATAFORMA; (ii) imposibilitar la emisión de tickets o la confirmación de asistencia a eventos; (iii) impedir la generación de facturas y el procesamiento de pagos; o (iv) afectar negativamente la calidad y oportunidad en la prestación de los servicios.

En los casos en que la finalidad del tratamiento esté basada en el consentimiento del TITULAR, la negativa a entregar los datos o su revocatoria no afectará el acceso a los servicios esenciales de la PLATAFORMA sustentados en otras bases de legitimación.

## 11. Terceros con acceso a datos, transferencias y destinatarios de datos personales.

### 11.1. Casos en que MULTITICKETING permitirá el acceso de terceros a los datos personales.

MULTITICKETING podrá permitir el acceso o transferir los datos personales del TITULAR de conformidad con lo establecido en esta POLÍTICA, en los contratos suscritos con los terceros y de conformidad con la REGULACIÓN. El TITULAR entiende y acepta que MULTITICKETING podrá compartir su información personal con personas naturales o jurídicas, ubicadas en la República del Ecuador o en el extranjero, en los siguientes casos:

**11.1.1.** Cuando actúen como encargados del tratamiento de datos personales, prestando servicios por cuenta y bajo instrucciones documentadas de MULTITICKETING, para cumplir con las finalidades que se detallan en esta POLÍTICA.

**11.1.2.** Cuando sea necesario realizar una transferencia o comunicación a un tercero que actúe como destinatario de los datos, de conformidad con la REGULACIÓN.

### 11.2. Categorías de terceros que podrán tener acceso o recibir los datos personales del TITULAR.

A continuación, se detallan las categorías de terceros que podrán tener acceso o recibir los datos personales del TITULAR:

**11.2.1.** Aliados estratégicos:

- Organizadores de eventos que contraten los servicios de MULTITICKETING para la gestión y ejecución de eventos a través de la PLATAFORMA.
- Entidades o empresas relacionadas con la implementación de campañas, programas e iniciativas vinculadas a los eventos gestionados por MULTITICKETING.

**11.2.2.** Proveedores de servicios tecnológicos y operativos:

- Proveedores de servicios de alojamiento en la nube (hosting), procesamiento y almacenamiento de datos, destinados a la operación técnica de la PLATAFORMA. Actualmente, MULTITICKETING utiliza los servicios de Sevalla by Kinsta y Google Cloud.
- Proveedores de pasarelas de pago para el procesamiento de transacciones económicas derivadas de la adquisición de tickets y demás servicios. Actualmente, MULTITICKETING utiliza los servicios de Payphone.
- Proveedores de servicios de envío de notificaciones por correo electrónico, aplicaciones de mensajería u otros canales habilitados para la comunicación con los TITULARES.

### 11.3. Casos en los que no se necesitará el consentimiento para revelar los datos.

MULTITICKETING se compromete a no divulgar o compartir los datos personales del TITULAR sin que éste haya prestado previamente su consentimiento para ello. Se exceptúan, sin embargo, los siguientes casos:

- Requerimientos de información de autoridades administrativas o judiciales, en ejercicio de sus funciones.
- Requerimientos de autoridades administrativas que tengan por objeto el tratamiento posterior de datos con fines históricos, estadísticos o científicos, siempre y cuando dichos datos se encuentren debidamente disociados o al menos anonimizados.
- En general, solicitudes o deberes de entrega de información, fundamentados en disposiciones legales y obligaciones contractuales adquiridas con el TITULAR u otra base de legitimación aplicable según corresponda.
- Cuando aplique otra base de legitimación prescrita en la REGULACIÓN.

## 12. Transferencias internacionales de datos personales.

MULTITICKETING informa al TITULAR que, en virtud de la utilización de proveedores de servicios tecnológicos cuyos servidores se encuentran ubicados fuera del territorio ecuatoriano, los datos personales del TITULAR podrán ser objeto de transferencia internacional. En particular, los servicios de alojamiento en la nube proporcionados por Sevalla by Kinsta y Google Cloud implican el almacenamiento y procesamiento de datos en servidores ubicados en los Estados Unidos de América.

Estas transferencias internacionales se realizarán en conformidad con la normativa vigente y las disposiciones emitidas por la Superintendencia de Protección de Datos Personales en materia de transferencias internacionales de datos personales, garantizando en todo momento un nivel adecuado de protección de los datos del TITULAR. MULTITICKETING ha suscrito los instrumentos contractuales correspondientes con sus encargados del tratamiento ubicados en el extranjero, a fin de asegurar el cumplimiento de las obligaciones establecidas en la REGULACIÓN.

## 13. Medidas de seguridad.

MULTITICKETING implementará las medidas técnicas y organizativas apropiadas para garantizar un nivel adecuado de seguridad acorde con el riesgo, conforme a lo establecido en la REGULACIÓN. Dichas medidas tienen por objeto proteger los datos personales del TITULAR contra el acceso, uso, modificación, divulgación o destrucción no autorizados, así como garantizar la confidencialidad, integridad y disponibilidad de la información.

Entre las medidas implementadas se incluyen, sin limitación, las siguientes:

- Uso de protocolos de cifrado para la transmisión de datos a través de la PLATAFORMA.
- Controles de acceso lógico basados en roles y perfiles de usuario.
- Almacenamiento de datos en servidores seguros con respaldos periódicos.
- Políticas internas de gestión de contraseñas y autenticación.
- Capacitación del personal en materia de protección de datos personales.
- Suscripción de acuerdos de confidencialidad con el personal y con los encargados del tratamiento.
- Evaluaciones periódicas de seguridad de la información. MULTITICKETING revisará y actualizará periódicamente las medidas de seguridad implementadas, a fin de adecuarlas a los estándares tecnológicos vigentes y a las disposiciones de la REGULACIÓN.

## 14. Decisiones automatizadas y elaboración de perfiles.

MULTITICKETING informa al TITULAR que la PLATAFORMA permite la configuración de perfiles de usuario, mediante los cuales los TITULARES pueden seleccionar un perfil público o privado para hacer networking de acuerdo con sus preferencias. Esta funcionalidad se basa en la información proporcionada voluntariamente por el TITULAR y no implica la elaboración automatizada de perfiles con efectos jurídicos o significativos para el TITULAR.

MULTITICKETING no realiza, en la actualidad, procesos de scoring, profiling ni decisiones automatizadas que produzcan efectos jurídicos sobre el TITULAR o le afecten significativamente. En la eventualidad de que en el futuro se implementen este tipo de procesos, MULTITICKETING informará previamente al TITULAR y garantizará su derecho a obtener intervención humana, a expresar su punto de vista y a impugnar la decisión, conforme a lo establecido en la REGULACIÓN.

## 15. Cookies y tecnologías de rastreo.

La PLATAFORMA podrá utilizar cookies y otras tecnologías de rastreo similares para mejorar la experiencia de navegación del TITULAR, facilitar el funcionamiento técnico de la PLATAFORMA, recopilar información estadística sobre el uso del sitio web y personalizar el contenido mostrado al TITULAR.

El TITULAR podrá configurar su navegador para rechazar todas o algunas cookies, o para recibir un aviso cuando se envíe una cookie. No obstante, si el TITULAR desactiva o rechaza las cookies, es posible que algunas funcionalidades de la PLATAFORMA no estén disponibles o no funcionen correctamente.

Para mayor información sobre el uso de cookies, el TITULAR podrá consultar la política de cookies publicada en la PLATAFORMA.

## 16. Derechos del titular de los datos.

El TITULAR podrá ejercer los derechos sobre sus datos personales enviando una solicitud a la dirección de correo electrónico info@redplanettribe.org.

### 16.1. Contenido de la Solicitud.

La Solicitud deberá incluir como mínimo lo siguiente:

**16.1.1.** El nombre y dirección de correo electrónico del TITULAR o cualquier otro medio para recibir respuestas.

**16.1.2.** Un documento que acredite la identidad del solicitante y, de ser el caso, la de su representante con la respectiva autorización.

**16.1.3.** La descripción clara y precisa de los datos personales respecto de los cuales el TITULAR busca ejercer alguno de los derechos establecidos en la Ley Orgánica de Protección de Datos Personales.

**16.1.4.** La petición concreta.

### 16.2. Derechos que podrán ser ejercidos por el TITULAR.

Con relación al tratamiento de sus datos personales, los TITULARES podrán ejercer los siguientes derechos:

- Derecho de acceso: el TITULAR podrá solicitar información sobre si sus datos personales están siendo tratados por MULTITICKETING, así como obtener una copia de los mismos.
- Derecho de rectificación: el TITULAR podrá solicitar la corrección o actualización de sus datos personales cuando éstos sean inexactos, incompletos o desactualizados.
- Derecho de eliminación o supresión: el TITULAR podrá solicitar la eliminación de sus datos personales cuando, entre otras razones, ya no sean necesarios para las finalidades para las cuales fueron recogidos, haya revocado su consentimiento, o los datos hayan sido tratados ilícitamente.
- Derecho de oposición: el TITULAR podrá oponerse al tratamiento de sus datos personales cuando dicho tratamiento se base en el interés legítimo de MULTITICKETING, o cuando los datos se traten con fines de mercadotecnia directa.
- Derecho a la portabilidad: el TITULAR podrá solicitar la entrega de sus datos personales en un formato compatible, actualizado, estructurado, común, interoperable y de lectura mecánica.
- Derecho a la limitación del tratamiento: el TITULAR podrá solicitar que se suspenda el tratamiento de sus datos personales en los supuestos previstos en la REGULACIÓN. MULTITICKETING atenderá las solicitudes de los TITULARES dentro de los plazos y en las condiciones establecidas por la REGULACIÓN. En caso de que la solicitud no pueda ser atendida total o parcialmente, MULTITICKETING informará al TITULAR de los motivos de su negativa, los cuales podrán estar fundados en la existencia de obligaciones legales, intereses legítimos prevalecientes, o cualquier otra causa establecida en la REGULACIÓN.

## 17. Revocatoria del consentimiento.

El TITULAR podrá revocar la autorización y consentimiento otorgado a MULTITICKETING para una o varias finalidades del tratamiento de sus datos personales, mediante la presentación de una solicitud escrita remitida a la dirección de correo electrónico info@redplanettribe.org.

## 18. Portabilidad de datos personales.

Previa solicitud expresa y por escrito del TITULAR, que deberá ser remitida a la dirección de correo electrónico info@redplanettribe.org, MULTITICKETING podrá proporcionar sus datos personales en un formato compatible, actualizado, estructurado, común, interoperable y de lectura mecánica, preservando sus características; para que puedan ser custodiados por el propio TITULAR o ser entregados a otro responsable definido por el TITULAR.

## 19. Legislación aplicable.

Los términos contenidos en esta POLÍTICA se rigen y se interpretan de acuerdo con las leyes vigentes en la República del Ecuador.

## 20. Consentimiento.

El TITULAR declara haber sido informado y acepta, sin reserva ni condicionamiento alguno, las declaraciones y compromisos que aquí se contienen. En particular, acepta y aprueba de forma expresa, libre, específica, inequívoca e informada el manejo de sus datos personales por parte de MULTITICKETING, en la forma y con los alcances que se detallan en la POLÍTICA.

## 21. Uso de medios electrónicos.

El TITULAR confiere su consentimiento para el uso de medios electrónicos y tecnológicos en su relación con MULTITICKETING, para lo cual se compromete a actuar de buena fe y con fines legales y lícitos. En dicho sentido, el TITULAR ratifica, acepta y declara irrevocablemente que las instrucciones y comunicaciones realizadas a MULTITICKETING a través de medios electrónicos, informáticos y telemáticos, como internet, correo electrónico, formularios electrónicos, y demás mecanismos habilitados por MULTITICKETING, son plenamente válidas y vinculantes.

Además, el TITULAR realiza las siguientes declaraciones: (i) Tiene suficiente capacidad legal y se encuentra facultado para realizar las declaraciones contenidas en esta cláusula y consecuentemente obligarse con MULTITICKETING, por lo que es plenamente responsable de sus alcances y efectos. (ii) Las claves, códigos y demás medidas de seguridad son utilizados como métodos de validación electrónica, tienen los efectos legales establecidos por la Ley de Comercio Electrónico, Firma Electrónica y Mensajes de Datos, su Reglamento y demás normativa que sea competente. (iii) Reconoce y acepta de forma expresa que la POLÍTICA será vinculante y ejecutable, desde su aceptación que podrá ser emitida a través de los medios electrónicos habilitados por MULTITICKETING.

## 22. Reformas.

MULTITICKETING se reserva el derecho a actualizar, modificar o eliminar esta POLÍTICA en cualquier tiempo, ya sea total o parcialmente. Toda modificación será publicada o notificada a los TITULARES a través de la PLATAFORMA u otros canales que MULTITICKETING habilite con ese fin. Tales reformas entrarán en vigor y su aplicación será obligatoria a partir de esta notificación o publicación.$legal$ FROM policy_versions v WHERE v.label = '1';

-- terms edition "1": 4 artifacts, 2 per language.
INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'label-terms-acceptance', 1, $legal$I have read and accept the General Terms and Conditions of **Multiticketing**.$legal$ FROM terms_versions v WHERE v.label = '1';
INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'en', 'terms', 2, $legal$# GENERAL TERMS AND CONDITIONS OF MULTITICKETING

> **Courtesy translation.** The authoritative text of these Terms is the Spanish one, published at the same address in Spanish. This English version is provided so that English-speaking readers can understand what they are accepting; in the event of any discrepancy between the two, the Spanish version prevails (section 37).

These Terms govern access to and use of Multiticketing. The organizer, and not Multiticketing except where it organizes an event of its own, offers and delivers the event to the attendee. Multiticketing retains responsibility for the technology, account, QR, collection, communication and support services, and for any other actions it effectively controls.

## 1. Identification of the operator

The platform known as "Multiticketing" is currently operated and administered by FUNDACIÓN REDPLANETTRIBE, an Ecuadorian private, social, non-profit organization, with taxpayer number (RUC) 1793228468001 (hereinafter, "Multiticketing", the "Foundation" or the "Operator").

Contractual address: Av. Portugal y Av. 6 de Diciembre, Plaza Arts, Quito, Ecuador, in accordance with the registered address on file.

Support and complaints: info@redplanettribe.org.

The Foundation operates Multiticketing within its institutional purposes and devotes its resources exclusively to fulfilling them. For as long as the Foundation is the Operator, the Platform may not be used for party-political or religious activities incompatible with its nature and bylaws.

Any change of operator will be communicated in advance, will identify the new provider, will preserve acquired rights, valid tickets, payments and outstanding obligations, and will collect fresh acceptance where the change is material or where the applicable relationship requires it.

## 2. Purpose, scope and contractual documents

These Terms set out the common rules and the specific rules that apply according to the way in which each person uses Multiticketing: visitor, attendee, buyer, organizer, representative of an organization, staff, sponsor or authorized user. The same person may take on several roles; in that case the rules of each role apply simultaneously in respect of the corresponding conduct.

Multiticketing may comprise, depending on the module enabled for each event: public discovery pages; registration and accounts; ticketing for free or paid events; ticket assignment; a dynamic personal QR; tools for organization, agenda, sessions, communications, chat and networking; technological tools for entry, exit and re-entry control managed by the organizer; management of staff and sponsors through the platform; metrics and reports; external links; and technological facilitation of payments through third parties.

The following form part of the relationship, as applicable: (i) these Terms; (ii) the offer and purchase summary shown before a transaction is confirmed; (iii) the event's particular conditions supplied by the organizer and expressly accepted by the user who buys tickets for that event; (iv) the conditions of the payment provider or of an external site, for its own service; and (v) the Privacy Policy in force, solely in respect of the processing of personal data. Acceptance of these Terms is contractual and is kept separate from the optional privacy consents.

These Terms do not retroactively modify prices, benefits or rights already acquired.

## 3. Acceptance and electronic contracting

Acceptance will be given by means of a mandatory, separate, un-preticked checkbox accompanied by a direct link to the full text in Spanish. Accessing or browsing the Platform, receiving an email, opening a link or continuing to use it evidences the express acceptance previously required.

The user accepts these Terms when creating or enabling their account and, where a materially modified version is introduced, in order to continue using the Platform. The organizer must accept them expressly in the capacity of organizer before publishing their first event, and each representative declares that they have sufficient authority to bind the person or entity they represent. Likewise, staff must accept them when enabling their account or permissions. If direct sponsor access is enabled in the future, the sponsor must accept before using it.

General acceptance will not be used as a substitute for the affirmative statement tied to the purchase of, or registration for, an event. Before completing a purchase or registration, the user must check the particular conditions supplied by the organizer and affirmatively declare that they accessed the applicable version. Multiticketing will retain the exact version tied to the event and to the transaction, and no dynamic link may alter what was accepted.

## 4. Definitions

* Attendee: a person who registers for or intends to take part in an event, including a guest to whom a ticket is assigned.

* Buyer: a person who starts or completes the acquisition of one or more tickets, for themselves or for guests.

* Organizer: a natural or legal person who creates, publishes, offers, administers or delivers an event and defines its particular conditions. Where the Foundation publishes an event of its own, it will also act as organizer in respect of that event.

* Staff: an individual person invited and authorized by the organizer to operate event functions. That fact alone does not make them part of Multiticketing's personnel.

* Sponsor: a sponsor linked to the event by the organizer, whose brand, offer, content or participation may be displayed or managed through the Platform.

* Account: an individual profile authenticated by email and a one-time code or another enabled mechanism.

* Ticket: the right or authorization to register for and access an event, associated with an account and verifiable through the personal QR. The QR is not, by itself, the ticket.

* Personal QR: a dynamic, individual and non-shareable identifier of the account, which allows the associated authorizations to be looked up when it is scanned by authorized personnel.

* Particular conditions: the organizer's information and rules on price, date, venue, admission, benefits, restrictions, changes, cancellation, returns, refunds and other aspects of the event.

## 5. Description of the service and Multiticketing's role

Multiticketing supplies technological infrastructure to publish and discover events; create accounts; register, buy, assign and validate tickets; enable communications, networking, agenda and management features; display sponsors; produce metrics; and facilitate payments or transfers according to the enabled method. The modules may be used together or independently; for example, ticketing may operate without agenda, chat or networking.

Multiticketing does not ordinarily direct the logistics, venue, permits, capacity, security, content, agenda, benefits, admission or delivery of the event. Those matters fall to the organizer. Multiticketing will be responsible for its own conduct, including the reasonable operation of the account and QR, the information it publishes itself, the technological processing it controls, the collection or settlement it actually carries out, its charges, its receipts and its support.

Where payment is processed by PayPhone under the Foundation's merchant account and the funds initially enter the Foundation, Multiticketing takes part as a technology provider and as the operational recipient of the funds for reconciliation and subsequent settlement to the organizer, in addition to charging its service fee. This intervention is not described as mere brokerage and will be subject to the flow disclosed before the purchase.

## 6. Types of users, roles and representation

Visitors may consult public pages without creating an account, but may only carry out the actions that the interface permits without authentication. The assignment and use of a ticket by means of a QR requires an individual account. If the Platform enables a purchase before registration, the buyer must complete the creation or linking of an account and the applicable acceptance before using the ticket.

Anyone acting on behalf of a legal person or organization declares that they have sufficient authorization. Multiticketing may request reasonable information to verify the representation and may suspend organizing powers while an objective doubt exists. The organizer is responsible for the selection, invitation, permissions and acts carried out by their staff and other delegated users within the scope granted to them.

Consumer status will be determined by the specific purpose of the transaction. Consumer protection rules will apply to the attendee or final buyer where appropriate; relationships entered into by an organizer, sponsor or business client for their own activity will additionally be governed by commercial rules and by any B2B agreements that may be signed.

## 7. Capacity, access requirements and accounts

Only persons who have reached eighteen years of age may buy through Multiticketing. The user declares that they meet this requirement. Additional restrictions of age or capacity for an event, drink, prize draw, area or benefit are the organizer's responsibility and must be stated in the particular conditions and verified at the event.

The account is individual. The user must provide accurate and up-to-date information; safeguard access to their email, device and OTP codes; not share codes or sessions; close unrecognized sessions; and immediately notify support of any unauthorized access. Acts carried out from an authenticated account may be attributed to the account holder, save for timely notice and evidence to the contrary.

For as long as the email remains available, access can be recovered by OTP. If that email is lost, Multiticketing will provide best-efforts assistance to try to verify ownership through reasonable and secure information, but the current system does not guarantee recovery. This limitation does not by itself extinguish payment rights; that is, the user must immediately contact support and the organizer to seek an operational solution before the event.

## 8. Common obligations of users

Every user undertakes to: use the Platform in good faith and in accordance with the law; keep their information genuine; respect third-party rights; comply with the conditions applicable to their role and to the event; protect their account and QR; review operational notices; use support channels responsibly; and cooperate reasonably in preventing fraud, improper access and security incidents.

The user may not impersonate others, share the account or QR, alter receipts, interfere with validations, automate the extraction of information, evade access controls, introduce harmful code, overload the infrastructure, harass, discriminate, threaten, send spam, infringe rights, publish unlawful content or use the Platform for purposes incompatible with the Foundation — for as long as it is the Operator.

## 9. Rules applicable to organizers

The organizer is the offeror and the party principally responsible for the event towards attendees and buyers. On publishing, they declare that they can bind themselves or the identified entity, and they assume responsibility for: the offer, base price, capacity, categories, benefits, restrictions, particular conditions, invoicing of the event, permits, venue, accessibility, safety, emergencies, capacity limits, admission, agenda, guests, alcohol, promotions, prize draws, sponsors, staff, content, changes, cancellation, rescheduling, returns, refunds, and the event's other obligations.

Before publishing, the organizer will provide Multiticketing with their verifiable legal or trade name; a current direct channel for contact with attendees and buyers; a full description; date, time and venue; price and currency; capacity and categories; admission requirements; and any material restriction. The information will be clear, genuine and verifiable, and will be kept up to date.

The organizer must deal directly with enquiries and complaints relating to the event and issue the user the receipt corresponding to the event service. They may not use these Terms to reduce their own obligations, to shift to Multiticketing matters under their control, or to impose on the consumer waivers that are prohibited. They will be liable to Multiticketing for damage directly arising from their breach, to the extent permitted by law and without affecting third-party rights.

## 10. Publication and administration of events

The organizer may create, configure, edit and cancel events and, depending on the module, manage capacity, waiting lists, VIP access, areas, activities with limited places, re-entries, visibility, attendee approval, agenda, sessions, chat, sponsors, deliverables, team and reports. The existence of a tool does not mean that it is enabled for every event.

Multiticketing does not pre-approve every event. It may review, request corrections, decline to publish, deactivate or withdraw an event where there is a breach of these Terms, incomplete information, or where it identifies misleading information, risk to users or infrastructure, an order from an authority, infringement of rights, fraud, unlawful activity or objective incompatibility with the Foundation's purposes. Where possible, it will state the cause and allow correction before any definitive measure, save in cases of urgency or serious risk.

Technical deactivation does not by itself terminate contracts already entered into with attendees. The organizer must inform and fulfil their outstanding obligations, and Multiticketing will preserve the records and channels necessary within its control.

## 11. Organizer information and particular conditions

Each event page must identify the organizer and display, before purchase or registration, a direct channel supplied by them. The organizer warrants that this information is genuine and current. Multiticketing will apply reasonable controls, but does not warrant data it has been unable to verify; this does not remove its duty to correct or withdraw information when it becomes aware that it is inaccurate.

The attendee must contact the organizer to check all the particular conditions before making the affirmative statement; those conditions are distinct from these Terms and from PayPhone's conditions. The record of acceptance will link user or buyer, event, order, role, version, date and time.

The organizer may not retroactively modify the price, benefits or rights already acquired. If they modify the date, place, agenda, guests, benefits, requirements or any material condition, they must communicate this in good time and offer the remedies provided for in their conditions and by law. The user's mere duty to review information does not turn silence or continued use into acceptance of a material modification.

## 12. Rules applicable to attendees and buyers

Before registering or buying, the attendee must review the organizer's identity and contact details, the final price, date, venue, requirements, benefits, particular conditions and the change and refund policy. The affirmative statement confirms access to that information; it does not constitute a waiver of mandatory rights, nor does it cure information that is missing, false, illegible or altered.

The attendee will provide correct information for their account and for the assignment of tickets, will check that the ticket type and the guests are correct, and will attend to confirmation links in good time. Errors attributable to incorrect data may delay the assignment. Multiticketing may correct them on request where technically possible, without assuming the cost or responsibility for the event, which it did not cause.

Requests concerning logistics, admission, changes, cancellation, benefits, invoicing of the event and refunds after the period enabled in the payment method must be addressed to the organizer. Complaints about the account, QR, technological assignment, service fee, Multiticketing's own receipt, security or the functioning of Multiticketing will be addressed to its support channel.

## 13. Registration, purchase and assignment of tickets

Events may be free, paid, or may refer the user to an external site. Registration or payment does not guarantee the ticket until the published conditions are met, the applicable transaction is confirmed and the ticket appears associated with the account. In external purchases, the third party and the organizer govern the external transaction; Multiticketing is responsible only for the link or service it actually supplies.

A person may acquire several tickets. For guests, they must enter the required identifiers and emails. Each guest will receive a link and must sign in or create an account, accept the applicable documents and confirm the assignment. If they do not complete the process, the ticket will not be associated with their QR and cannot be validated at the event.

The current functionality allows the buyer to designate or replace guests. That power must be exercised in good faith, in accordance with the event's conditions, without resale or fraud and respecting rights already acquired by third parties. Reassignment does not make the personal QR transferable and may not be used to duplicate access or to disregard a validation already used.

## 14. The personal QR: nature, use and restrictions

Each account has a dynamic personal QR. The QR identifies the account and allows a check of whether a valid ticket or authorization exists for the event, session, area, deliverable or benefit. A separate QR is not necessarily generated for each event, and the QR, in isolation from the associated authorization, does not constitute a ticket.

The QR is personal and may not be shared, sold, reproduced for third parties or used from someone else's account. A screenshot may not work, because the identifier is dynamic. The person must display it from the application or enabled channel and must have internet access and a compatible device, unless the organizer announces an alternative.

Multiticketing and the organizer may block an account or authorization in cases of fraud, duplication, shared use, valid revocation, chargeback, breach or objective risk, each within their own remit. Suspending an account is not automatically equivalent to cancelling a paid ticket, and must be applied proportionately, with information and a route to review where possible.

## 15. Access, capacity, re-entry, areas and activities

Authorized staff scan the QR and validate the registered authorization. The organizer determines and enforces the rules on doors, identity, capacity, re-entry, areas, schedules, sessions, waiting lists, VIP access, deliverables and activities with limited places, in accordance with the offer and the particular conditions. Places may not be sold in excess of the applicable capacity.

Multiticketing's digital records are operational evidence, but they do not replace legitimate security or admission checks, nor do they authorize arbitrary decisions. A refusal attributable to the event's conditions is the organizer's; a refusal caused by an error of Multiticketing's own account, QR or records will be handled by Multiticketing.

Validation requires connectivity for the attendee and for staff under the current flow. The organizer must provide for a reasonable contingency procedure for a live event. The absence of an alternative does not authorize valid tickets to be disregarded, nor a failure controlled by the organizer to be shifted onto the user.

## 16. Prices, service fee, taxes and payment methods

Before a purchase is confirmed, the final amount will be shown in United States dollars, with the base price of the tickets, the service fee, the VAT applicable to that fee and any other permitted item. No cost that has not been disclosed and accepted will be added afterwards.

The organizer sets the base price. Multiticketing sets its service fee; under the arrangement currently disclosed, the standard fee is ten per cent (10%) of the base price, plus the VAT applicable to the fee, unless a different configuration or agreement is disclosed before the purchase. The organizer chooses whether the fee is added to the base price and borne by the buyer, or deducted from the amount the organizer receives. The screen shown before payment determines the binding amount for that transaction.

Where PayPhone is used, the payment form is hosted in PayPhone's environment and its own terms govern the financial processing. Multiticketing does not store or tokenize card data. The merchant account is in the Foundation's name and the current receipt identifies Multiticketing as the recipient of the payment.

The Platform may also enable a direct transfer to an account of the organizer. In that case, Multiticketing does not receive the funds, acts exclusively on the organizer's instruction to associate the ticket, and will not itself verify the proof of payment. If the organizer does not confirm, Multiticketing will report the lack of confirmation and direct the buyer to the organizer's channel, without prejudice to its support regarding the account.

## 17. Payment confirmation, reconciliation and settlement

For payments through PayPhone, the funds initially enter the Foundation. Payment confirmation and assignment may be automatic or may require human review. Multiticketing reconciles the transaction, withholds its fee and transfers to the organizer the balance due when the organizer requests a withdrawal or in accordance with the enabled frequency, once they have recorded valid bank details.

Multiticketing will inform the organizer of the available status of sales, withdrawals and transfers, and may request information to resolve payments that have failed or been duplicated, reversed, fraudulent or subject to chargeback. It may not appropriate the organizer's amounts beyond the fee and deductions previously disclosed or legally payable. The commercial settlement terms may additionally be documented in an order or B2B agreement.

Confirmation of receipt, an automatic email or the opening of a message do not by themselves substitute for acceptance or for the final confirmation of a purchase. The user must check the summary and the status of the ticket in their account.

## 18. Invoicing and financial responsibilities

The organizer will issue the buyer the invoice or receipt corresponding to the event service. The Foundation will issue the corresponding receipt solely for its service fee. If the Foundation organizes an event of its own, it will also assume the invoicing of the event in that capacity.

The organizer is responsible for the accuracy of their tax details, prices, taxes and obligations relating to the event. Multiticketing is responsible for its fee, its receipt, the reconciliation and the transfers it actually handles. Nothing in these Terms substitutes for the tax and accounting validation that falls to each party.

## 19. Cancellations, changes, returns and refunds

The organizer must have a cancellation and refund policy and must administer it directly. Cancellation, rescheduling, change of venue, substantial reduction of benefits, overselling, unjustified refusal of access or defective performance will be resolved in accordance with the offer, the particular conditions and the consumer's mandatory rights. No clause may exclude those rights in advance.

In the PayPhone flow, the user may request a voluntary reversal from the Platform until 20:00 on the same day as the transaction, subject to the payment provider's availability and rules. After that limit, the organizer directly handles and carries out any refund and its costs. For a mass cancellation, the organizer orders and carries out the return; Multiticketing will provide the relevant operational information about attendees and the communications within its control.

Responsibility for the service fee in a cancellation, mandatory return or failed performance will be resolved according to the cause, the offer, the law and the party that performed or breached the service. Multiticketing will not use this section to withhold a fee where it must legally be returned, nor may the organizer refuse a refund by referring the user to the Platform.

## 20. Sponsors, advertising, promotions and trademarks

The event functionality allows the incorporation of sponsors, featured profiles, logos, offers, deliverables, interactions and reports managed by the organizer. The sponsor currently contracts with the organizer and not, by that fact alone, with Multiticketing. The organizer uploads and approves their content, manages their communications and shares the relevant reports.

The organizer warrants that they have sufficient authorization and/or rights over the trademarks, images, advertisements, products, services, promotions, prize draws and materials they provide to Multiticketing. They must disclose conditions, availability and restrictions, and are liable for misleading advertising, infringements and delivery of benefits. Multiticketing may withdraw content where it becomes aware of an infringement, where there is an order from a competent authority or where these Terms are breached.

Multiticketing does not guarantee leads, sales, meetings, exposure or commercial results.

## 21. Staff and operational permissions

The organizer or their delegates invite individual staff accounts and configure their permissions. Depending on the configuration, staff may validate entries and sessions, record deliverables, moderate chat, view or manage questions, mark answers, update the status of sessions and consult operational data of speakers or authorized contacts. Shared credentials are not permitted.

Staff act on the organizer's behalf. The organizer must instruct them, limit their permissions to what is necessary, revoke access when their function ends, and be responsible for their door and management decisions. Multiticketing may technically suspend a staff account for misuse or risk, but this does not make staff its employees, nor does it assume the physical delivery of the event.

## 22. Profiles, networking, community and user content

The user may voluntarily activate, edit or hide a public profile with the information the interface permits, including job title, company, LinkedIn, website, interests, what they are looking for and what they offer. Visibility and messaging permissions may depend on the event and on the ticket type configured by the organizer. General event messages may be visible to everyone who is registered.

Harassment, threats, discrimination, spam, impersonation, scraping, misleading communications, unlawful content, unauthorized dissemination of information and any conduct that affects the security or experience of others are prohibited. Users may block or report where the feature exists and, in any case, may write to support. The organizer reviews the event's networking incidents, and Multiticketing handles those that affect the Platform or breach these Terms.

Multiticketing facilitates connections, but does not guarantee contacts, meetings, leads, sales, hires, partnerships or results. Closing an account does not extinguish outstanding obligations, nor does it authorize the deletion of evidence needed for purchases, complaints or legal compliance.

## 23. Multiticketing's intellectual property and licence of use

The Platform, its software, interfaces, documentation, structural databases, institutional content and distinctive signs are protected by the applicable rules and belong to Multiticketing or to their respective owners and licensors, as applicable. These Terms do not assert exclusive ownership that is not evidenced, nor do they transfer rights to the user.

While the account is in force, Multiticketing grants the user permission to access and use the enabled features in accordance with these Terms. It is not permitted to copy, exploit, decompile, circumvent controls, create derivative works or use Multiticketing's signs beyond what is permitted by law or by written authorization.

## 24. Content and rights of organizers, attendees and sponsors

Each user retains the rights over the content they upload. They grant Multiticketing, for as long as is necessary to provide the service and to fulfil outstanding obligations, a non-exclusive, territorially sufficient, royalty-free, limited licence, revocable so far as compatible with operations, to host, technically reproduce, adapt to formats, communicate and display that content at the event and in the configured channels.

The user warrants authorship, ownership or authorization, and that the content does not infringe rights, the law, public order or these Terms. The licence does not authorize Multiticketing to exploit the content for purposes unrelated to the service. Takedown requests for infringement must be sent to info@redplanettribe.org identifying the complainant, the work or sign, the location of the material, the grounds and a means of contact.

## 25. Prohibited conduct and acceptable use

In addition to the prohibitions above, it is prohibited to: create fictitious events; publish false prices, benefits or capacities; take payments without the ability to perform; evade disclosed fees; resell or duplicate tickets without authorization; manipulate metrics; obtain data through unauthorized automation; offer alcohol, prize draws or restricted benefits without conditions and controls; infringe licences; test vulnerabilities without permission; and use the Platform for party-political or religious activities incompatible with the Foundation for as long as it is the Operator.

Moderation measures will be based on seriousness, recurrence, risk and role. They may comprise a warning, withdrawal of content, functional limitation, temporary suspension, blocking of the QR, deactivation of an event or termination. Where there is no urgency, fraud, risk or legal impediment, the cause and a channel for review will be communicated.

## 26. Third-party services, links, components and licences

Multiticketing uses third-party services, currently including PayPhone for payments; Resend, AWS SES or other services for email; Google Cloud, Firebase and Google services for infrastructure or authentication; and software components subject to their own licences. Use of a specific third party's service may be subject to its terms, which must be presented separately where they impose obligations on the user.

Links to external registrations, purchases or sites will be identified as such. Multiticketing does not control their content or their transactions, and will be responsible for its own selection, integration or information only to the extent of its participation. Open-source and third-party licences will be respected in accordance with their texts; the inclusion of a component does not automatically make the whole Platform open-source software, nor does it authorize the use of third-party trademarks.

## 27. Metrics, reports and analytics

Depending on the module enabled and the event's configuration, Multiticketing may display metrics on visits, conversion, registrations, sales, affiliate links, entries, exits, re-entries, times, sessions, areas, deliverables, sponsors and interactions, and may allow exports in the available formats. Standalone ticketing is limited to metrics relating to tickets and digital channels; the management features widen the scope only when they are enabled.

The metrics are operational and of reasonable accuracy. They may be affected by connectivity, missed or duplicated scanning, configuration, devices, staff conduct, reversed payments or incomplete data. They therefore do not constitute a guarantee of attendance, revenue, compliance, leads or results. The organizer must check and retain the reports they need before their access is closed.

## 28. Availability, maintenance, support and contingencies

Multiticketing will seek to provide reasonable availability, without promising absolute continuity or a service level that has not been agreed. It may carry out maintenance, improvements, corrections and technical changes that do not alter essential rights. If scheduled maintenance may materially affect a live event, it will seek to give at least one week's notice; urgent incidents may be dealt with without prior notice.

The service depends on the internet, devices and external providers. Multiticketing will be responsible for the diligence required of it in its own systems and integrations; the organizer will be responsible for connectivity and contingency at the venue. In an outage, the organizer must verify tickets and restore the service, without imposing on the attendee the automatic loss of a valid ticket.

Support handles problems with OTP, accounts, the QR, technological assignment, checkout and the functioning of the Platform through info@redplanettribe.org. No specific response times are promised unless and until they are published or expressly agreed.

## 29. Suspension and termination of accounts, events or services

Multiticketing may suspend or terminate access for material breach, fraud, impersonation, a shared QR, abusive chargebacks, security risk, unlawful use, infringement of rights, failure to pay B2B obligations, an order from an authority, damage to the infrastructure or repeated offences. The measure must be proportionate and limited, where possible, to the account, feature or event affected.

The organizer may refuse or revoke an authorization for the causes provided for in their particular conditions and by law, but may not alter the account's general records nor use a revocation to avoid returns. Multiticketing will not reverse a valid operational decision of the organizer regarding a ticket, save to correct a technical error of its own, to comply with an order or to protect mandatory rights.

The user may request closure of their account. They are warned that without an account there will be no accessible QR, and that outstanding tickets, payments, receipts and complaints must be resolved first. Closure does not extinguish acquired rights or outstanding obligations, and Multiticketing will retain the records it is required to keep. If closure prevents access to a live event, the user must immediately contact support and the organizer.

## 30. Responsibility of the parties

Multiticketing's responsibility: it comprises the diligent provision of its technological services; the generation and functioning of the QR and the records under its control; reasonable account security; its own information and advertising; correction of its own errors; the service fee and the corresponding receipt; the receipt, reconciliation and settlement of funds it handles; support; and compliance with binding decisions within its sphere.

The organizer's responsibility: it comprises the whole event, such as the offer, conditions, base price, invoicing of the event, permits, venue, capacity, accessibility, safety, staff, sponsors, content, admission, changes, cancellation, rescheduling, returns, refunds and dealing directly with complaints about the event.

The user's responsibility: it comprises the accuracy of their data, safeguarding their account and QR, lawful use, compliance with access conditions and their own conduct.

Neither party will be liable for a breach caused exclusively by force majeure or an act of God for as long as it is unforeseeable and irresistible, provided they take reasonable mitigation measures and give notice where possible. Force majeure does not release a party from returning amounts where the law, the nature of the obligation or a service not provided so requires.

## 31. Warranties and permitted limits

Multiticketing does not warrant that all modules will always be enabled, that every account will be recoverable without sufficient evidence, that an event will take place, that the organizer will admit a person who fails valid requirements, or that networking, metrics or sponsors will produce results. These exclusions do not cover defects, wilful misconduct, negligence, its own breaches or the consumer's mandatory rights.

Any limitation of damages will apply solely in B2B relationships and to the extent permitted by law and by a negotiated agreement. Liability is not limited for personal injury, fraud, infringement of rights, improper handling of funds, obligations to make returns, its own defective performance or any case that cannot be excluded.

## 32. Support, complaints and content takedown

Complaints relating to Multiticketing may be submitted to info@redplanettribe.org. At least the complainant's identity and contact details, the date, the event or order, a description and the request will be recorded, and a record will be provided on request. Multiticketing may ask for information strictly necessary to verify the account or the incident.

Complaints about the event will be referred to the organizer's direct channel shown on the page, without preventing the user from informing Multiticketing of a lack of response, fraud, misleading information or risk. Multiticketing will not close a complaint of its own merely because an organizer is also involved.

## 33. Modification of the Terms and changes to the service

Multiticketing may update these Terms to reflect changes in law, operator, security, payments or features. Non-material changes may be communicated through the Platform or by email and will apply going forward. Material changes affecting obligations, liability, payments, dispute resolution, contractual privacy, access or rights will require prominent notice and fresh express acceptance before continuing to use the affected feature.

No update will retroactively alter a purchase, ticket, price, benefit or right that has been accepted. If the user does not accept a material version, they may stop using future features; Multiticketing will adopt a proportionate consequence and will allow prior tickets, payments, receipts and obligations to be resolved.

## 34. Versions, evidence and a true copy

Multiticketing will identify each version and will retain the exact text or a verifiable fingerprint, together with the evidence of acceptance. The record must include, depending on the act, the date and time, the user and email, the version, the role, and the technical data reasonably necessary for attribution, integrity and proof.

After accepting, the user will have access to a true and downloadable copy from their account; the text in force and the version history will also be available outside the account in the legal center. The version of the particular conditions accepted for a purchase will be retained statically and linked to the transaction.

Pre-existing accounts must accept this version prospectively in order to continue using the Platform. Organizers with events already published must accept before new publications or material management actions, without affecting prior contracts or retroactively shifting responsibilities.

## 35. Privacy Policy and communications

The processing of personal data is governed by the Privacy Policy in force, available in Multiticketing's legal center. These Terms do not replace that policy.

Messages that are indispensable for OTP, security, purchases, tickets, event changes, support and contractual performance are operational communications.

## 36. Governing law and dispute resolution

These Terms are governed by Ecuadorian law. Where a consumer is involved, the competent authorities and courts will be those corresponding to their domicile and to mandatory rules. No mediation or arbitration will be binding on the consumer without express, separate and unequivocal acceptance.

In B2B relationships, the parties will seek to resolve the dispute in good faith and may voluntarily resort to mediation. Failing a different written agreement, and without affecting mandatory jurisdiction, non-consumer disputes will be heard before the competent courts of Quito, Ecuador.

## 37. Final provisions

The nullity or unenforceability of a provision will not affect the remainder; the clause will be interpreted in the valid manner closest to its purpose, without reducing consumer rights. Failure to exercise a right does not constitute a waiver. The headings are for ease of reading and do not limit the scope of the text.

The Spanish version prevails over any translation. Contractual notices will be sent to the registered email or displayed in the account, without prejudice to any additional notices required. The user must keep their email up to date and Multiticketing must keep its support channel operational.

An assignment or change of operator may not be used to diminish acquired rights, release outstanding obligations or conceal the provider's identity. Where it is material, it will require prior notice and fresh acceptance.$legal$ FROM terms_versions v WHERE v.label = '1';
INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'label-terms-acceptance', 1, $legal$He leído y acepto los Términos y Condiciones Generales de **Multiticketing**.$legal$ FROM terms_versions v WHERE v.label = '1';
INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
SELECT v.id, 'es', 'terms', 2, $legal$# TÉRMINOS Y CONDICIONES GENERALES DE MULTITICKETING

Estos Términos regulan el acceso y uso de Multiticketing. El organizador, y no Multiticketing salvo cuando organice un evento propio, ofrece y ejecuta el evento frente al asistente. Multiticketing conserva responsabilidad por los servicios tecnológicos, de cuenta, QR, cobro, comunicación, soporte y demás actuaciones que efectivamente controle.

## 1. Identificación del operador

La plataforma denominada “Multiticketing” es actualmente operada y administrada por FUNDACIÓN REDPLANETTRIBE, organización ecuatoriana privada, social y sin fines de lucro, con RUC 1793228468001 (en adelante, “Multiticketing”, la “Fundación” o el “Operador”).

Domicilio contractual: Av. Portugal y Av. 6 de Diciembre, Plaza Arts, Quito, Ecuador, conforme al domicilio estatutario informado.

Soporte y reclamos: info@redplanettribe.org.

La Fundación opera Multiticketing dentro de sus fines institucionales y destina sus recursos exclusivamente para su cumplimiento. Mientras la Fundación sea el Operador, la Plataforma no podrá utilizarse para actividades político-partidistas o religiosas incompatibles con su naturaleza y estatuto.

Cualquier cambio de operador se comunicará previamente, identificará al nuevo proveedor, preservará los derechos adquiridos, las entradas vigentes, los pagos y obligaciones pendientes, y recabará nueva aceptación cuando el cambio sea material o lo exija la relación aplicable.

## 2. Objeto, alcance y documentos contractuales

Estos Términos establecen las reglas comunes y las reglas específicas aplicables según la forma en que cada persona use Multiticketing: visitante, asistente, comprador, organizador, representante de una organización, staff, sponsor o usuario autorizado. Una misma persona puede asumir varios roles; en tal caso le aplican simultáneamente las reglas de cada rol respecto de la actuación correspondiente.

Multiticketing puede comprender, según el módulo habilitado para cada evento: páginas públicas de descubrimiento; registro y cuentas; ticketera para eventos gratuitos o pagados; asignación de entradas; QR personal dinámico; herramientas de organización, agenda, sesiones, comunicaciones, chat y networking; herramientas tecnolgócias para el control de ingreso, salida y reingreso gestionado por el organizador; gestión de staff y sponsors a través de la plataforma; métricas y reportes; enlaces externos; y facilitación tecnológica de pagos mediante terceros.

Forman parte de la relación, en lo que corresponda: (i) estos Términos; (ii) la oferta y resumen de compra mostrados antes de confirmar una transacción; (iii) las condiciones particulares del evento que suministre el organizador y expresamente aceptadas por el usuario que compre entradas para ese evento; (iv) las condiciones del proveedor de pago o de un sitio externo, para su propio servicio; y (v) la Política de Privacidad vigente, únicamente respecto del tratamiento de datos personales. La aceptación de estos Términos es contractual y se mantiene separada de los consentimientos opcionales de privacidad.

Estos Términos no modifican retroactivamente precios, beneficios ni derechos ya adquiridos.

## 3. Aceptación y contratación electrónica

La aceptación se realizará mediante una casilla obligatoria, separada, no premarcada y acompañada de un enlace directo al texto completo en español. Acceder o navegar por la Plataforma, recibir un correo, abrir un enlace o continuar usándola evidencia la aceptación expresa previamente requerida.

El usuario acepta estos Términos al crear o habilitar su cuenta y, cuando se introduzca una versión materialmente modificada, para continuar usando la Plataforma. El organizador debe aceptarlos expresamente en calidad de organizador antes de publicar su primer evento y cada representante declara tener facultades suficientes para obligar a la persona o entidad que representa. De igual manera, el staff debe aceptarlos al habilitar su cuenta o permisos. Si en el futuro se habilita acceso directo a un sponsor, este deberá aceptar antes de usarlo.

La aceptación general no se utilizará como sustituto de la manifestación afirmativa vinculada a la compra o registro de un evento. Antes de finalizar una compra o registro, el usuario deberá verificar las condiciones particulares suministradas por el organizador y declarar afirmativamente que accedió a la versión aplicable. Multiticketing conservará la versión exacta vinculada al evento y a la operación, sin que un enlace dinámico pueda alterar lo aceptado.

## 4. Definiciones

* Asistente: persona que se registra o pretende participar en un evento, incluida la persona invitada a quien se asigna una entrada.

* Comprador: persona que inicia o completa la adquisición de una o varias entradas, para sí o para invitados.

* Organizador: persona natural o jurídica que crea, pública, ofrece, administra o ejecuta un evento y define sus condiciones particulares. Cuando la Fundación publique un evento propio, actuará también como organizador respecto de ese evento.

* Staff: persona individual invitada y autorizada por el organizador para operar funciones del evento. No forma parte del personal de Multiticketing por ese solo hecho.

* Sponsor: auspiciante vinculado al evento por el organizador, cuya marca, oferta, contenido o participación puede mostrarse o gestionarse mediante la Plataforma.

* Cuenta: perfil individual autenticado mediante correo electrónico y código de un solo uso u otro mecanismo habilitado.

* Entrada o ticket: derecho o habilitación de registro y acceso a un evento, asociado a una cuenta y validable mediante el QR personal. El QR no es, por sí solo, la entrada.

* QR personal: identificador dinámico, individual y no compartible de la cuenta, que permite consultar las habilitaciones asociadas cuando es escaneado por personal autorizado.

* Condiciones particulares: información y reglas del organizador sobre precio, fecha, sede, admisión, beneficios, restricciones, cambios, cancelación, devolución, reembolso y demás aspectos del evento.

## 5. Descripción del servicio y rol de Multiticketing

Multiticketing suministra infraestructura tecnológica para publicar y descubrir eventos; crear cuentas; registrar, comprar, asignar y validar entradas; habilitar comunicaciones, networking, agenda y funciones de gestión; mostrar sponsors; producir métricas; y facilitar pagos o transferencias conforme al medio habilitado. Los módulos pueden usarse conjuntamente o de forma independiente; por ejemplo, la ticketera puede operar sin agenda, chat o networking.

Multiticketing no dirige ordinariamente la logística, sede, permisos, aforo, seguridad, contenido, agenda, beneficios, admisión ni ejecución del evento. Esas materias corresponden al organizador. Multiticketing será responsable por sus propias actuaciones, incluida la operación razonable de la cuenta y QR, la información que publique por sí misma, el procesamiento tecnológico que controle, el cobro o liquidación que efectivamente realice, sus cargos, sus comprobantes y su soporte.

Cuando el pago se procesa por PayPhone bajo la cuenta comercial de la Fundación y los fondos ingresan inicialmente a esta, Multiticketing participa como proveedor tecnológico y receptor operativo de los fondos para su conciliación y posterior liquidación al organizador, además de cobrar su cargo de servicio. Esta intervención no se describe como simple corretaje y se sujetará al flujo informado antes de la compra.

## 6. Tipos de usuarios, roles y representación

Los visitantes pueden consultar páginas públicas sin crear cuenta, pero solo podrán realizar las actuaciones que la interfaz permita sin autenticación. La asignación y uso de una entrada mediante QR exige una cuenta individual. Si la Plataforma habilita una compra antes del registro, el comprador deberá completar la creación o vinculación de cuenta y la aceptación aplicable antes de usar la entrada.

Quien actúe por una persona jurídica u organización declara que cuenta con autorización suficiente. Multiticketing podrá solicitar información razonable para verificar la representación y suspender facultades organizativas mientras exista una duda objetiva. El organizador responde por la selección, invitación, permisos y actos realizados por su staff y demás usuarios delegados dentro del ámbito que les conceda.

La condición de consumidor se determinará por la finalidad concreta de la operación. Las reglas protectoras del consumidor se aplicarán al asistente o comprador final cuando corresponda; las relaciones celebradas por un organizador, sponsor o cliente empresarial para su actividad se regirán además por las reglas mercantiles y los acuerdos B2B que se llegasen a suscribir.

## 7. Capacidad, requisitos de acceso y cuentas

Solo las personas que hayan cumplido dieciocho años pueden comprar mediante Multiticketing. El usuario declara que cumple este requisito. Las restricciones adicionales de edad o capacidad para un evento, bebida, sorteo, zona o beneficio son responsabilidad del organizador y deberán constar en las condiciones particulares y verificarse en el evento.

La cuenta es individual. El usuario deberá proporcionar información exacta y actualizada; custodiar el acceso a su correo, dispositivo y códigos OTP; no compartir códigos ni sesiones; cerrar sesiones no reconocidas; y comunicar inmediatamente a soporte cualquier acceso no autorizado. Los actos realizados desde una cuenta autenticada podrán atribuirse al titular, salvo aviso oportuno y prueba en contrario.

Mientras el correo siga disponible, el acceso puede recuperarse mediante OTP. Si se pierde ese correo, Multiticketing brindará asistencia de medios para intentar verificar la titularidad mediante información razonable y segura, pero el sistema actual no asegura la recuperación. Esta limitación no extingue por sí sola derechos de pago, es decir, el usuario deberá contactar inmediatamente a soporte y al organizador para buscar una solución operativa antes del evento.

## 8. Obligaciones comunes de los usuarios

Todo usuario se obliga a: utilizar la Plataforma de buena fe y conforme a la ley; mantener información auténtica; respetar los derechos de terceros; cumplir las condiciones aplicables al rol y al evento; proteger la cuenta y el QR; revisar avisos operativos; utilizar canales de soporte de manera responsable; y colaborar razonablemente en la prevención de fraude, accesos indebidos e incidentes de seguridad.

El usuario no podrá suplantar identidades, compartir la cuenta o QR, alterar comprobantes, interferir con validaciones, automatizar extracción de información, evadir controles de acceso, introducir código dañino, sobrecargar la infraestructura, acosar, discriminar, amenazar, enviar spam, infringir derechos, publicar contenido ilícito o utilizar la Plataforma para fines incompatibles con la Fundación –mientras esta sea el Operador–.

## 9. Reglas aplicables a organizadores

El organizador es el oferente y responsable principal del evento frente a asistentes y compradores. Al publicar declara que puede obligarse por sí o por la entidad identificada y asume la responsabilidad por: oferta, precio base, cupos, categorías, beneficios, restricciones, condiciones particulares, facturación del evento, permisos, sede, accesibilidad, seguridad, emergencias, aforo, admisión, agenda, invitados, alcohol, promociones, sorteos, sponsors, staff, contenidos, cambios, cancelación, reprogramación, devoluciones, reembolsos, y demás obligaciones del evento.

Antes de publicar, el organizador entregará a Multiticketing su nombre legal o comercial verificable; canal directo vigente para contacto con asistentes y compradores; descripción completa; fecha, horario y sede; precio y moneda; cupos y categorías; requisitos de admisión; y cualquier restricción material. La información será clara, auténtica, comprobable y se mantendrá actualizada.

El organizador deberá atender directamente las consultas y reclamaciones relativas al evento y emitir al usuario el comprobante correspondiente al servicio del evento. No podrá utilizar estos Términos para reducir obligaciones propias, trasladar a Multiticketing hechos bajo su control ni imponer al consumidor renuncias prohibidas. Responderá frente a Multiticketing por daños directamente derivados de su incumplimiento, en la medida permitida por la ley y sin afectar derechos de terceros.

## 10. Publicación y administración de eventos

El organizador puede crear, configurar, editar y cancelar eventos, y, según el módulo, administrar aforo, listas de espera, accesos VIP, zonas, actividades con cupo, reingresos, visibilidad, aprobación de asistentes, agenda, sesiones, chat, sponsors, entregables, equipo y reportes. La existencia de una herramienta no significa que esté habilitada para todos los eventos.

Multiticketing no aprueba previamente todos los eventos. Puede revisar, solicitar correcciones, no publicar, desactivar o retirar un evento cuando exista incumplimiento de estos Términos, información incompleta o identifique información engañosa, riesgo para usuarios o infraestructura, orden de autoridad, infracción de derechos, fraude, actividad ilícita o incompatibilidad objetiva con los fines de la Fundación. Cuando sea posible, informará la causa y permitirá corregir antes de una medida definitiva, salvo urgencia o riesgo grave.

La desactivación técnica no resuelve por sí misma los contratos ya celebrados con asistentes. El organizador deberá informar y cumplir sus obligaciones pendientes, y Multiticketing preservará los registros y canales necesarios dentro de su control.

## 11. Información del organizador y condiciones particulares

Cada página de evento deberá identificar al organizador y mostrar, antes de la compra o registro, un canal directo suministrado por él. El organizador garantiza la autenticidad y vigencia de esa información. Multiticketing aplicará controles razonables, pero no garantiza datos que no haya podido verificar; ello no excluye su deber de corregir o retirar información cuando conozca su inexactitud.

El asistente deberá comunicarse con el organizador para verificar todas las condiciones particulares antes de la manifestación afirmativa; las cuáles serán distintas de estos Términos y de las condiciones de PayPhone. El registro de aceptación vinculará usuario o comprador, evento, pedido, rol, versión, fecha y hora.

El organizador no podrá modificar retroactivamente el precio, beneficios o derechos ya adquiridos. Si modifica fecha, lugar, agenda, invitados, beneficios, requisitos o cualquier condición material, deberá comunicarlo oportunamente y ofrecer los remedios previstos en sus condiciones y en la ley. La mera obligación del usuario de revisar información no convierte el silencio o uso continuado en aceptación de una modificación material.

## 12. Reglas aplicables a asistentes y compradores

Antes de registrarse o comprar, el asistente deberá revisar la identidad y contacto del organizador, el precio final, fecha, sede, requisitos, beneficios, condiciones particulares y política de cambios y reembolso. La manifestación afirmativa confirma acceso a esa información; no constituye renuncia a derechos imperativos ni sanea información ausente, falsa, ilegible o alterada.

El asistente proporcionará información correcta para su cuenta y para la asignación de entradas, verificará que el tipo de entrada y los invitados sean correctos y atenderá oportunamente los enlaces de confirmación. Los errores imputables a datos incorrectos podrán retrasar la asignación. Multiticketing podrá corregirlos a solicitud cuando sea técnicamente posible, sin asumir el costo o responsabilidad del evento que no haya causado.

Las solicitudes sobre logística, admisión, cambios, cancelación, beneficios, facturación del evento y reembolsos posteriores al período habilitado en el medio de pago deberán dirigirse al organizador. Las reclamaciones sobre cuenta, QR, asignación tecnológica, cargo de servicio, comprobante propio, seguridad o funcionamiento de Multiticketing se dirigirán a su canal de soporte.

## 13. Registro, adquisición y asignación de entradas

Los eventos pueden ser gratuitos, pagados o remitir a un sitio externo. El registro o pago no garantiza la entrada hasta que se cumplan las condiciones publicadas, se confirme la operación aplicable y la entrada figure asociada a la cuenta. En compras externas, el tercero y el organizador regulan la transacción externa; Multiticketing responde únicamente por el enlace o servicio que efectivamente suministre.

Una persona puede adquirir varias entradas. Para invitados deberá ingresar los identificadores y correos requeridos. Cada invitado recibirá un enlace y deberá iniciar sesión o crear cuenta, aceptar los documentos aplicables y confirmar la asignación. Si no completa el proceso, la entrada no se asociará a su QR y no podrá ser validada en el evento.

La funcionalidad actual permite al comprador designar o sustituir invitados. Esa facultad deberá ejercerse de buena fe, conforme a las condiciones del evento, sin reventa ni fraude y respetando los derechos ya adquiridos por terceros. La reasignación no vuelve transferible el QR personal y no podrá utilizarse para duplicar accesos ni desconocer una validación ya consumida.

## 14. QR personal: naturaleza, uso y restricciones

Cada cuenta posee un QR personal dinámico. El QR identifica la cuenta y permite consultar si existe una entrada o habilitación válida para el evento, sesión, zona, entregable o beneficio. No se genera necesariamente un QR distinto por evento y el QR, aislado de la habilitación asociada, no constituye una entrada.

El QR es personal y no puede compartirse, venderse, reproducirse para terceros ni utilizarse desde una cuenta ajena. Una captura de pantalla puede no funcionar porque el identificador es dinámico. La persona deberá mostrarlo desde la aplicación o canal habilitado y disponer de internet y de un dispositivo compatible, salvo que el organizador anuncie una alternativa.

Multiticketing y el organizador podrán bloquear una cuenta o habilitación ante fraude, duplicidad, uso compartido, revocación válida, contracargo, incumplimiento o riesgo objetivo, cada uno dentro de sus competencias. Una suspensión de cuenta no equivale automáticamente a cancelación de una entrada pagada y deberá aplicarse de forma proporcional, con información y vía de revisión cuando sea posible.

## 15. Acceso, aforo, reingreso, zonas y actividades

El staff autorizado escanea el QR y valida la habilitación registrada. El organizador determina y ejecuta las reglas de puerta, identidad, aforo, reingreso, zonas, horarios, sesiones, listas de espera, accesos VIP, entregables y actividades con cupo, conforme a la oferta y condiciones particulares. No podrán venderse cupos superiores a la capacidad aplicable.

Los registros digitales de Multiticketing son evidencia operativa, pero no sustituyen verificaciones legítimas de seguridad o admisión ni autorizan decisiones arbitrarias. Una negativa atribuible a condiciones del evento corresponde al organizador; una negativa causada por un error propio de cuenta, QR o registro de Multiticketing será atendida por Multiticketing.

La validación requiere conectividad para el asistente y el staff en el flujo actual. El organizador deberá prever un procedimiento de contingencia razonable para un evento activo. La ausencia de una alternativa no autoriza a desconocer entradas válidas ni a trasladar al usuario una falla controlada por el organizador.

## 16. Precios, cargo de servicio, impuestos y medios de pago

Antes de confirmar una compra se mostrará en dólares de los Estados Unidos de América el valor final, con el precio base de las entradas, el cargo de servicio, el IVA aplicable a dicho cargo y cualquier otro rubro permitido. No se añadirá después un costo que no haya sido informado y aceptado.

El organizador fija el precio base. Multiticketing fija su cargo de servicio; en la operación actualmente informada, el cargo estándar corresponde al diez por ciento (10 %) del precio base, más el IVA aplicable al cargo, salvo una configuración o acuerdo distinto informado antes de la compra. El organizador elige si el cargo se suma al precio base y lo asume el comprador o si se descuenta del valor que recibe el organizador. La pantalla previa al pago determinará el importe vinculante para esa operación.

Cuando se utilice PayPhone, el formulario de pago se aloja en el entorno de PayPhone y sus propios términos regulan el procesamiento financiero. Multiticketing no almacena ni tokeniza datos de tarjeta. La cuenta comercial está a nombre de la Fundación y el comprobante actual identifica a Multiticketing como destinatario del pago.

La Plataforma también podrá habilitar transferencia directa a una cuenta del organizador. En ese caso, Multiticketing no recibe los fondos, actúa exclusivamente por instrucción del organizador para asociar la entrada y no verificará por sí sola el comprobante. Si el organizador no confirma, Multiticketing informará la falta de confirmación y dirigirá al comprador al canal del organizador, sin perjuicio del soporte sobre la cuenta.

## 17. Confirmación, conciliación y liquidación de pagos

En pagos mediante PayPhone, los fondos ingresan inicialmente a la Fundación. La confirmación de pago y asignación puede ser automática o requerir revisión humana. Multiticketing concilia la operación, retiene su cargo y transfiere al organizador el saldo que corresponda cuando este solicite retiro o conforme a la periodicidad habilitada, una vez que haya registrado datos bancarios válidos.

Multiticketing informará al organizador el estado disponible de ventas, retiros y transferencias, y podrá solicitar información para resolver pagos fallidos, duplicados, reversados, fraudulentos o sujetos a contracargo. No podrá apropiarse de valores del organizador fuera del cargo y deducciones previamente informados o legalmente exigibles. Las condiciones comerciales de liquidación podrán documentarse adicionalmente en una orden o acuerdo B2B.

La confirmación de recepción, el correo automático o la apertura de un mensaje no sustituyen por sí solos la aceptación ni la confirmación final de una compra. El usuario deberá verificar el resumen y el estado de la entrada en su cuenta.

## 18. Facturación y responsabilidades económicas

El organizador emitirá al comprador la factura o comprobante correspondiente al servicio del evento. La Fundación emitirá el comprobante correspondiente únicamente a su cargo de servicio. Si la Fundación organiza un evento propio, asumirá también la facturación del evento en esa calidad.

El organizador es responsable por la veracidad de sus datos fiscales, precios, impuestos y obligaciones relacionadas con el evento. Multiticketing es responsable por su cargo, su comprobante, la conciliación y las transferencias que efectivamente gestione. Nada en estos Términos sustituye la validación tributaria y contable que corresponda a cada parte.

## 19. Cancelaciones, cambios, devoluciones y reembolsos

El organizador deberá contar con una política de cancelación y reembolso y atenderla directamente. La cancelación, reprogramación, cambio de sede, reducción sustancial de beneficios, sobreventa, negativa injustificada de acceso o prestación defectuosa se resolverá conforme a la oferta, las condiciones particulares y los derechos imperativos del consumidor. Ninguna cláusula puede excluir anticipadamente esos derechos.

En el flujo PayPhone, el usuario puede solicitar desde la Plataforma un reverso voluntario hasta las 20h00 del mismo día de la operación, sujeto a la disponibilidad y reglas del proveedor de pago. Después de ese límite, el organizador gestiona y ejecuta directamente cualquier reembolso y sus costos. Para una cancelación masiva, el organizador ordena y ejecuta la devolución; Multiticketing facilitará la información operativa de asistentes que corresponda y las comunicaciones dentro de su control.

La responsabilidad sobre el cargo de servicio en una cancelación, devolución obligatoria o prestación fallida se resolverá conforme a la causa, la oferta, la ley y la parte que haya prestado o incumplido el servicio. Multiticketing no utilizará esta sección para retener un cargo cuando legalmente deba restituirse, ni el organizador podrá negar reembolso por remitir al usuario a la Plataforma.

## 20. Sponsors, publicidad, promociones y marcas

La funcionalidad de eventos permite incorporar sponsors, perfiles destacados, logotipos, ofertas, entregables, interacciones y reportes gestionados por el organizador. El sponsor contrata actualmente con el organizador y no con Multiticketing por ese solo hecho. El organizador carga y aprueba su contenido, gestiona sus comunicaciones y comparte los reportes que correspondan.

El organizador garantiza que tiene autorización y/o derechos suficientes sobre marcas, imágenes, anuncios, productos, servicios, promociones, sorteos y materiales que proporcione a Multiticketing. Deberá informar condiciones, disponibilidad y restricciones, y responder por publicidad engañosa, infracciones y cumplimiento de beneficios. Multiticketing podrá retirar contenido cuando conozca una infracción, exista orden competente o se incumplan estos Términos.

Multiticketing no garantiza leads, ventas, reuniones, exposición ni resultados comerciales.

## 21. Staff y permisos operativos

El organizador o sus delegados invitan cuentas individuales de staff y configuran sus permisos. Según la configuración, el staff puede validar ingresos y sesiones, registrar entregables, moderar chat, visualizar o gestionar preguntas, marcar respuestas, actualizar el estado de sesiones y consultar datos operativos de speakers o contactos autorizados. No se permiten credenciales compartidas.

El staff actúa por cuenta del organizador. El organizador deberá instruirlo, limitar sus permisos a lo necesario, revocar accesos al terminar la función y responder por sus decisiones de puerta y gestión. Multiticketing podrá suspender técnicamente una cuenta de staff por uso indebido o riesgo, pero no convierte al staff en su dependiente ni asume la ejecución física del evento.

## 22. Perfiles, networking, comunidad y contenido de usuarios

El usuario puede activar, editar u ocultar voluntariamente un perfil público con la información que la interfaz permita, incluidos cargo, empresa, LinkedIn, sitio web, intereses, lo que busca y lo que ofrece. La visibilidad y los permisos de mensajería pueden depender del evento y del tipo de entrada configurado por el organizador. Los mensajes generales del evento pueden ser visibles para todas las personas registradas.

Se prohíben acoso, amenazas, discriminación, spam, suplantación, scraping, comunicaciones engañosas, contenido ilícito, difusión no autorizada de información y cualquier conducta que afecte la seguridad o experiencia de terceros. Los usuarios podrán bloquear o reportar cuando la función exista y, en todo caso, escribir a soporte. El organizador revisa las incidencias de networking del evento y Multiticketing atiende aquellas que afecten la Plataforma o infrinjan estos Términos.

Multiticketing facilita conexiones, pero no garantiza contactos, reuniones, leads, ventas, contrataciones, alianzas ni resultados. El cierre de cuenta no extingue obligaciones pendientes ni autoriza a borrar evidencia necesaria para compras, reclamos o cumplimiento legal.

## 23. Propiedad intelectual de Multiticketing y licencia de uso

La Plataforma, su software, interfaces, documentación, bases estructurales, contenidos institucionales y signos distintivos están protegidos por la normativa aplicable y pertenecen a Multiticketing o a sus respectivos titulares y licenciantes, según corresponda. Estos Términos no afirman una titularidad exclusiva que no se encuentre acreditada ni transfieren derechos al usuario.

Mientras la cuenta esté vigente, Multiticketing concede al usuario un permiso para acceder y usar las funcionalidades habilitadas conforme a estos Términos. No se permite copiar, explotar, descompilar, eludir controles, crear obras derivadas o utilizar signos de Multiticketing fuera de lo permitido por la ley o una autorización escrita.

## 24. Contenidos y derechos de organizadores, asistentes y sponsors

Cada usuario conserva los derechos sobre el contenido que carga. Otorga a Multiticketing, mientras sea necesario para prestar el servicio y cumplir obligaciones pendientes, una licencia no exclusiva, territorialmente suficiente, gratuita, limitada y revocable en lo compatible con la operación para alojar, reproducir técnicamente, adaptar a formatos, comunicar y mostrar ese contenido en el evento y canales configurados.

El usuario garantiza autoría, titularidad o autorización y que el contenido no infringe derechos, ley, orden público ni estos Términos. La licencia no autoriza a Multiticketing a explotar el contenido para fines ajenos al servicio. Las solicitudes de retiro por infracción deberán enviarse a info@redplanettribe.org con identificación del reclamante, obra o signo, ubicación del material, fundamento y medio de contacto.

## 25. Conductas prohibidas y uso aceptable

Además de las prohibiciones anteriores, queda prohibido: crear eventos ficticios; publicar precios, beneficios o aforos falsos; captar pagos sin capacidad de ejecutar; eludir cargos informados; revender o duplicar entradas sin autorización; manipular métricas; obtener datos mediante automatización no autorizada; ofrecer alcohol, sorteos o beneficios restringidos sin condiciones y controles; infringir licencias; probar vulnerabilidades sin permiso; y usar la Plataforma para actividades políticas partidistas o religiosas incompatibles con la Fundación mientras esta sea el Operador.

Las medidas de moderación se basarán en la gravedad, recurrencia, riesgo y rol. Podrán comprender advertencia, retiro de contenido, limitación funcional, suspensión temporal, bloqueo de QR, desactivación de evento o terminación. Cuando no exista urgencia, fraude, riesgo o impedimento legal, se informará la causa y un canal de revisión.

## 26. Servicios, enlaces, componentes y licencias de terceros

Multiticketing utiliza servicios de terceros, incluidos actualmente PayPhone para pagos; Resend, AWS SES u otros servicios para correo; Google Cloud, Firebase y servicios de Google para infraestructura o autenticación; y componentes de software sujetos a licencias propias. El uso del servicio específico de un tercero puede quedar sujeto a sus términos, que deberán presentarse de forma separada cuando impongan obligaciones al usuario.

Los enlaces a registros, compras o sitios externos se identificarán como tales. Multiticketing no controla su contenido ni sus transacciones, y responderá por la selección, integración o información propia únicamente en la medida de su participación. Las licencias de código abierto o de terceros se respetarán conforme a sus textos; la inclusión de un componente no convierte automáticamente toda la Plataforma en software de código abierto ni autoriza el uso de marcas ajenas.

## 27. Métricas, reportes y analítica

Según el módulo habilitado y la configuración del evento, Multiticketing puede mostrar métricas de visitas, conversión, registros, ventas, enlaces de afiliados, entradas, salidas, reingresos, horarios, sesiones, zonas, entregables, sponsors e interacciones, y permitir exportaciones en formatos disponibles. La ticketera independiente se limita a métricas relacionadas con tickets y canales digitales; las funciones de gestión amplían el alcance solo cuando estén habilitadas.

Las métricas son operativas y de exactitud razonable. Pueden verse afectadas por conectividad, escaneo omitido o duplicado, configuración, dispositivos, actuación del staff, pagos revertidos o datos incompletos. Por tanto, no constituyen garantía de aforo, recaudación, cumplimiento, leads ni resultados. El organizador deberá verificar y conservar los reportes necesarios antes del cierre de su acceso.

## 28. Disponibilidad, mantenimiento, soporte y contingencias

Multiticketing procurará una disponibilidad razonable, sin prometer continuidad absoluta ni un nivel de servicio no pactado. Puede realizar mantenimiento, mejoras, correcciones y cambios técnicos que no alteren derechos esenciales. Si un mantenimiento programado puede afectar materialmente un evento activo, procurará avisar con al menos una semana de anticipación; las incidencias urgentes podrán atenderse sin aviso previo.

El servicio depende de internet, dispositivos y proveedores externos. Multiticketing responderá por la diligencia exigible en sus propios sistemas e integraciones; el organizador responderá por conectividad y contingencia en sede. En una caída, el organizador deberá verificar entradas y restablecer el servicio, sin imponer al asistente la pérdida automática de una entrada válida.

El soporte atiende problemas de OTP, cuenta, QR, asignación tecnológica, checkout y funcionamiento de la Plataforma a través de info@redplanettribe.org. No se prometen tiempos de respuesta específicos mientras no se publiquen o pacten expresamente.

## 29. Suspensión y terminación de cuentas, eventos o servicios

Multiticketing podrá suspender o terminar acceso por incumplimiento material, fraude, suplantación, QR compartido, contracargo abusivo, riesgo de seguridad, uso ilícito, infracción de derechos, falta de pago de obligaciones B2B, orden de autoridad, daño a la infraestructura o reincidencia. La medida deberá ser proporcional y limitarse, cuando sea posible, a la cuenta, función o evento afectado.

El organizador podrá negar o revocar una habilitación por las causas previstas en sus condiciones particulares y la ley, pero no podrá alterar los registros generales de la cuenta ni usar una revocación para evitar devoluciones. Multiticketing no revertirá una decisión operativa válida del organizador sobre un ticket, salvo para corregir un error técnico propio, cumplir una orden o proteger derechos imperativos.

El usuario puede solicitar cierre de cuenta. Se advierte que sin cuenta no habrá QR accesible y que deben resolverse entradas, pagos, comprobantes y reclamos vigentes. El cierre no extingue derechos adquiridos ni obligaciones pendientes y Multiticketing conservará los registros exigibles. Si el cierre impide el acceso a un evento vigente, el usuario deberá contactar inmediatamente a soporte y al organizador.

## 30. Responsabilidad de las partes

Responsabilidad de Multiticketing: comprende la prestación diligente de sus servicios tecnológicos; generación y funcionamiento del QR y registros bajo su control; seguridad razonable de la cuenta; información y publicidad propias; corrección de errores propios; cargo de servicio y comprobante correspondiente; recepción, conciliación y liquidación de fondos que gestione; soporte; y cumplimiento de decisiones obligatorias dentro de su esfera.

Responsabilidad del organizador: comprende todo el evento, como la oferta, condiciones, precio base, facturación del evento, permisos, sede, aforo, accesibilidad, seguridad, staff, sponsors, contenido, admisión, cambios, cancelación, reprogramación, devoluciones, reembolsos y atención directa de reclamos del evento.

Responsabilidad del usuario: comprende la exactitud de datos, custodia de cuenta y QR, uso lícito, cumplimiento de condiciones de acceso y actuaciones propias..

Ninguna parte responderá por un incumplimiento causado exclusivamente por fuerza mayor o caso fortuito mientras sea imprevisible e irresistible, adopte medidas razonables de mitigación e informe cuando sea posible. La fuerza mayor no libera de restituir valores cuando la ley, la naturaleza de la obligación o el servicio no prestado lo exijan.

## 31. Garantías y límites permitidos

Multiticketing no garantiza que todos los módulos estén siempre habilitados, que toda cuenta sea recuperable sin evidencia suficiente, que un evento se ejecute, que el organizador admita a una persona que incumple requisitos válidos ni que el networking, las métricas o los sponsors produzcan resultados. Estas exclusiones no cubren defectos, dolo, culpa, incumplimientos propios ni derechos obligatorios del consumidor.

Cualquier limitación de daños se aplicará únicamente en relaciones B2B y en la medida permitida por la ley y por un acuerdo negociado. No se limita la responsabilidad por daños personales, fraude, infracción de derechos, manejo indebido de fondos, obligaciones de devolución, prestación defectuosa propia o cualquier supuesto que no pueda excluirse.

## 32. Soporte, reclamos y retiro de contenido

Los reclamos relativos a Multiticketing podrán presentarse a info@redplanettribe.org. Se registrará al menos la identidad y contacto del reclamante, fecha, evento o pedido, descripción y solicitud, y se entregará constancia cuando se solicite. Multiticketing podrá pedir información estrictamente necesaria para verificar la cuenta o incidente.

Los reclamos sobre el evento se remitirán al canal directo del organizador mostrado en la página, sin impedir que el usuario informe a Multiticketing una falta de respuesta, fraude, información engañosa o riesgo. Multiticketing no cerrará un reclamo propio por el solo hecho de que también intervenga un organizador.

## 33. Modificación de los Términos y cambios del servicio

Multiticketing puede actualizar estos Términos para reflejar cambios legales, de operador, seguridad, pagos o funcionalidades. Los cambios no materiales podrán comunicarse mediante la Plataforma o correo y regirán hacia el futuro. Los cambios materiales que afecten obligaciones, responsabilidades, pagos, solución de controversias, privacidad contractual, acceso o derechos requerirán información destacada y nueva aceptación expresa antes de continuar usando la función afectada.

Ninguna actualización alterará retroactivamente una compra, entrada, precio, beneficio o derecho aceptado. Si el usuario no acepta una versión material, podrá dejar de usar funciones futuras; Multiticketing adoptará una consecuencia proporcionada y permitirá resolver entradas, pagos, comprobantes y obligaciones anteriores.

## 34. Versiones, evidencia y copia fiel

Multiticketing identificará cada versión y conservará el texto exacto o una huella verificable, junto con la evidencia de aceptación. El registro deberá incluir, según el acto, fecha y hora, usuario y correo, versión, rol, y datos técnicos razonablemente necesarios para atribución, integridad y prueba.

Después de aceptar, el usuario tendrá acceso a una copia fiel y descargable desde su cuenta; el texto vigente y el historial también estarán disponibles fuera de la cuenta en el centro legal. La versión de las condiciones particulares aceptadas para una compra se conservará estática y vinculada a la operación.

Las cuentas preexistentes deberán aceptar prospectivamente esta versión para continuar usando la Plataforma. Los organizadores con eventos ya publicados deberán aceptar antes de nuevas publicaciones o gestiones materiales, sin afectar contratos previos ni trasladar retroactivamente responsabilidades.

## 35. Política de Privacidad y comunicaciones

El tratamiento de datos personales se rige por la Política de Privacidad vigente disponible en el centro legal de Multiticketing. Estos Términos no sustituyen esa política.

Los mensajes indispensables para OTP, seguridad, compras, entradas, cambios del evento, soporte y ejecución contractual son comunicaciones operativas.

## 36. Legislación aplicable y solución de controversias

Estos Términos se rigen por la legislación ecuatoriana. Si interviene un consumidor, serán competentes las autoridades y jueces que correspondan conforme a su domicilio y a las normas imperativas. Ninguna mediación o arbitraje será obligatorio para el consumidor sin una aceptación expresa, separada e inequívoca.

En relaciones B2B, las partes procurarán resolver de buena fe la controversia y podrán acudir voluntariamente a mediación. A falta de acuerdo escrito distinto y sin afectar fueros imperativos, las controversias no consumidoras se conocerán ante los jueces competentes de Quito, Ecuador.

## 37. Disposiciones finales

La nulidad o inaplicabilidad de una disposición no afectará las restantes; la cláusula se interpretará en la medida válida más cercana a su finalidad, sin reducir derechos del consumidor. La falta de ejercicio de un derecho no constituye renuncia. Los títulos facilitan la lectura y no limitan el alcance del texto.

La versión en español prevalece sobre cualquier traducción. Las notificaciones contractuales se enviarán al correo registrado o se mostrarán en la cuenta, sin perjuicio de los avisos adicionales exigibles. El usuario deberá mantener su correo actualizado y Multiticketing deberá mantener vigente su canal de soporte.

Una cesión o cambio de operador no podrá utilizarse para disminuir derechos adquiridos, liberar obligaciones pendientes ni ocultar la identidad del proveedor. Cuando sea material, requerirá información previa y nueva aceptación.$legal$ FROM terms_versions v WHERE v.label = '1';

-- THE PROOF, AND THE REASON THIS FILE CAN BE TRUSTED.
--
-- The copy above is proved AGAINST THE ROW, not against the artifacts it was
-- copied from: for every edition that now has text, the SHA-256 recomputed over
-- the stored rows must equal the `content_hash` the version row has carried
-- since the day it was published. That is the stronger of the two available
-- properties, because the row's hash is what every Policy Acceptance and every
-- Terms Acceptance actually points at — proving the rows match the files would
-- only prove this migration copied its own input correctly.
--
-- THE PREIMAGE RULE, stated here once and implemented identically in Go
-- (internal/consent/legal.ContentHash):
--
--   * Languages in ASCENDING LOCALE CODE order ('en' before 'es'), compared as
--     bytes (COLLATE "C") so no database's collation can reorder them. This is
--     a rule now, not the coincidence it used to be: the Go code walked a
--     hand-pinned []Locale{en, es}.
--   * Each language's CODE is emitted as its own framed field, before that
--     language's artifacts.
--   * Then that language's artifacts in ORDINAL order, each as a framed field.
--   * A framed field is its BYTE length (octet_length, which is Go's
--     len(string)), a newline, and the field. Framing is what stops text moving
--     between two artifacts without moving the fingerprint.
--
-- A mismatch RAISES, which rolls this whole file back — the tables, the text
-- and the schema_migrations row with it, because the runner applies one file in
-- one transaction (internal/platform/migrate/migrate.go). A half-copied legal
-- corpus is not a state worth having: a database that refuses to start is a
-- problem somebody fixes, and a policy page serving text whose hash does not
-- match the acceptance evidence is a problem nobody notices.
--
-- IF THIS FAILS ON A DEVELOPMENT DATABASE, the answer is never to edit the row.
-- A `content_hash` a row already carries is the record of what a person was
-- shown, and rewriting one is the act that made the placeholder's hash
-- inconsistent between databases in the first place (see the note in migration
-- 060). Rebuild the database instead.
DO $verify$
DECLARE
    drift TEXT;
BEGIN
    SELECT string_agg(
               format('  %s edition %L: rows hash to %s, the row carries %s',
                      checked.document, checked.label, checked.computed, checked.content_hash),
               E'\n' ORDER BY checked.document, checked.label)
      INTO drift
      FROM (
          SELECT 'policy' AS document, v.label, v.content_hash,
                 encode(sha256(convert_to(preimage.text, 'UTF8')), 'hex') AS computed
            FROM policy_versions v
            JOIN LATERAL (
                SELECT string_agg(per_locale.framed, '' ORDER BY per_locale.locale COLLATE "C") AS text
                  FROM (
                      SELECT a.locale,
                             octet_length(a.locale)::text || E'\n' || a.locale
                                 || string_agg(octet_length(a.body)::text || E'\n' || a.body, '' ORDER BY a.ordinal) AS framed
                        FROM policy_version_artifacts a
                       WHERE a.version_id = v.id
                       GROUP BY a.locale
                  ) per_locale
            ) preimage ON TRUE
           WHERE preimage.text IS NOT NULL
          UNION ALL
          SELECT 'terms' AS document, v.label, v.content_hash,
                 encode(sha256(convert_to(preimage.text, 'UTF8')), 'hex') AS computed
            FROM terms_versions v
            JOIN LATERAL (
                SELECT string_agg(per_locale.framed, '' ORDER BY per_locale.locale COLLATE "C") AS text
                  FROM (
                      SELECT a.locale,
                             octet_length(a.locale)::text || E'\n' || a.locale
                                 || string_agg(octet_length(a.body)::text || E'\n' || a.body, '' ORDER BY a.ordinal) AS framed
                        FROM terms_version_artifacts a
                       WHERE a.version_id = v.id
                       GROUP BY a.locale
                  ) per_locale
            ) preimage ON TRUE
           WHERE preimage.text IS NOT NULL
      ) checked
     WHERE checked.computed IS DISTINCT FROM checked.content_hash;

    IF drift IS NOT NULL THEN
        RAISE EXCEPTION E'the legal text copied by migration 109 does not reproduce the fingerprint its edition was published under:\n%\n\nNothing has been written: this migration is rolled back whole. Do not edit the content_hash on a version row to make this pass — that hash is the record of what a person was shown.', drift;
    END IF;

    -- The edition a reader gets today must have text, in this database, now.
    -- The hash check above is silent about an edition with NO rows at all, and
    -- the one edition where that silence would be visible to a reader is the
    -- current one: the public policy and terms endpoints would answer every
    -- language with a 404. Editions this file does not know about — a scheduled
    -- one written after it, a label only one environment ever had — are
    -- deliberately left alone.
    IF NOT EXISTS (
        SELECT 1
          FROM policy_version_artifacts a
         WHERE a.version_id = (
                   SELECT id FROM policy_versions
                    WHERE effective_date <= CURRENT_DATE
                    ORDER BY effective_date DESC, created_at DESC
                    LIMIT 1)
    ) THEN
        RAISE EXCEPTION 'the current Policy Version has no text after migration 109: the public privacy policy would 404 in every language';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM terms_version_artifacts a
         WHERE a.version_id = (
                   SELECT id FROM terms_versions
                    WHERE effective_date <= CURRENT_DATE
                    ORDER BY effective_date DESC, created_at DESC
                    LIMIT 1)
    ) THEN
        RAISE EXCEPTION 'the current Terms Version has no text after migration 109: the public terms page would 404 in every language';
    END IF;
END
$verify$;
