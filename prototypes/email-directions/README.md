# PROTOTYPE: HTML mail visual directions (throwaway)

Question: which visual direction should the platform's HTML mail take?
This directory is a throwaway prototype on its own branch and must never reach `main` or the production mail path.

Three directions, each rendered as the Sale Confirmation and the Follow Digest, in ES and EN:

- **A - Card**: grey canvas, one white rounded card, Cover Image full-bleed across its top, like the Storefront's Event card. Digest rows are the Timeline card: text left, square Thumbnail right.
- **B - Ticket**: white page, centred, the purchase drawn as a ticket with a perforation and a big Reference stub. Digest rows lead with a date chip under tinted section bands; the Thumbnail hides below 620px.
- **C - Letter**: no canvas or card, a narrow left-aligned column that reads like a note. Facts as hairline rows, the Event with its Cover Image as the closing block. Digest is a dense list with the Thumbnail on the left.

## Run

```sh
python3 prototypes/email-directions/build.py && python3 -m http.server 64390 --bind 127.0.0.1 --directory prototypes/email-directions/dist
```

Open <http://localhost:64390/>.
The floating bar (and the arrow keys) switch direction, mail, language, Light / Dark (Apple) / Gmail invert, Desktop / 375px, images blocked, and a side-by-side Compare.
"Gmail invert" is an approximation of Gmail's forced dark mode: light backgrounds and dark text flip lightness, `background-image` and images are left alone.

To see them in a real mail UI, start a Mailpit and deliver all twelve:

```sh
docker run -d --name email-directions-mailpit -p 127.0.0.1:64825:8025 -p 127.0.0.1:64025:1025 axllent/mailpit:latest
python3 prototypes/email-directions/send_mailpit.py
```

Then open <http://localhost:64825/>; images load from the static server above, so keep it running.

## Files

- `build.py` writes `dist/` (12 mails plus the viewer). All copy is the existing EN/ES from `backend/internal/platform/email_content.go`, split into blocks; strings marked `NEW` are the only new ones.
- `make_images.py` generates the invented sample Cover Image, Thumbnails and Logos (no text baked in). `img/mark.png` is `apps/storefront/app/icon.svg` exported at 96px.
- `viewer.html` is the switcher page, copied to `dist/index.html`.
- `send_mailpit.py` delivers the built mails as multipart into Mailpit.

## Findings that hold for every direction

- Mailpit's HTML check scores all three alike (86-93% supported, about 1% unsupported, the same features: border-radius, background-image, `@media`, the preheader hiding), so compatibility does not separate them.
- Primary `#0d7bb3` gives 4.67:1 for white button text and for links on white, but only 4.15:1 on A's grey canvas, so footer links use the body text colour instead.
- With images blocked, fixed image cells keep their space: C's left Thumbnail column becomes a blank indent and an empty Logo tile stays visible as a bordered white square.
- The Logo tile stays white under Gmail-style inversion because it is painted with a `background-image` gradient, not only `background-color`.
- The existing consent and unsubscribe copy contains em dashes; the prototype splits those sentences instead.
- The Issuer's `razon_social` is stored in capitals (`FUNDACION REDPLANETTRIBE`), and the footer prints it as stored.

## Verdict (2026-11-23)

The user picked a mix:

- **Sale Confirmation: A - Card.** Grey canvas, white rounded card, Cover Image full-bleed across the top, a "Purchase confirmed" eyebrow, the Event name as the `<h1>`, then the greeting, the Reference/Total/Tax ID in a soft box, and a full-width primary button.
- **Follow Digest: C - Letter.** No canvas or card, a narrow left-aligned column, section headings over a heavy rule, and dense Event rows with the Thumbnail on the left and the call to action and "Because you follow" on one line.

Open for the spec, because the settled design asks for one shared layout on every mail and A and C have different chrome (A: grey canvas + card, 600px; C: plain white column, 560px):

- Which chrome is the shared layout, or whether the Cover Image kinds wear A's card and the rest wear C's plain column.
- Assumed until ruled: the other Cover Image kinds (Ticket Assignment, Sale Re-addressing) follow A, and the compact Event row kinds (Answer Reminder, Assignment Reminder, Sale Voided, Sale Reversal Refused, No Longer Holding) use C's row.
- C's left Thumbnail column leaves a blank indent when images are blocked; accept, or drop the cell when no rendition exists (already the rule) and live with the blocked-images case.
