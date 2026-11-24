# PROTOTYPE, throwaway. Not the production mail path.
#
# Question: which visual direction should the platform's HTML mail take?
# Three directions (A Card, B Ticket, C Letter), each rendered as the Sale
# Confirmation (wide Cover Image) and the Follow Digest (compact Event rows),
# in EN and ES. Output: dist/<direction>-<kind>-<locale>.html plus a viewer at
# dist/index.html (dark mode, Gmail-style inversion, 375px, images blocked).
#
# Everything is hand-written email HTML: tables, inline styles, 600px max, a
# <style> block only for dark mode and the mobile breakpoint. All people,
# Organizations and Events are invented.
import datetime as dt
import html
import os
import shutil

HERE = os.path.dirname(os.path.abspath(__file__))
DIST = os.path.join(HERE, "dist")

FONT = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
MONO = "ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono',monospace"
FG, MUTED, BORDER = "#121921", "#5c646f", "#dadee5"
CANVAS, PRIMARY, ACCENT, ACCENT_FG = "#eef2f7", "#0d7bb3", "#e8effc", "#1c2842"
SUCCESS = "#1f7a2c"

STOREFRONT = "https://discover.multiticketing.com"
TOKEN = "eyJzIjoiOWYyYzRlN2EtM2IxZC00YzJlLWE4ZjAtNmQ1YjdjOGUyZjEwIiwiZSI6MTc5NzI0MDAwMH0.q8Vb3nR2xLk7TfW1mZpA0sYd4uHcE6gJ"

WEEKDAYS_ES = ["lunes", "martes", "miércoles", "jueves", "viernes", "sábado", "domingo"]
MONTHS_ES = ["enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto",
             "septiembre", "octubre", "noviembre", "diciembre"]


def when(d, loc):
    """formatEventDate, mirrored: 'Saturday 12 December, 20:00' / 'sábado 12 de diciembre, 20:00'."""
    if loc == "es":
        return f"{WEEKDAYS_ES[d.weekday()]} {d.day} de {MONTHS_ES[d.month - 1]}, {d:%H:%M}"
    return d.strftime("%A ") + str(d.day) + d.strftime(" %B, %H:%M")


def chip(d, loc):
    mon = MONTHS_ES[d.month - 1][:3] if loc == "es" else d.strftime("%b")
    return str(d.day), mon.upper()


# ---------------------------------------------------------------- copy
# Existing copy from backend/internal/platform/email_content.go, broken into
# blocks. Strings marked NEW are the only new ones (preheaders, button labels,
# footer reason lines, the "or paste" fallback).
COPY = {
    "en": {
        "sale_subject": "Your tickets for {e}",
        "sale_pre": "Reference TP-7KQ2M9XA. Show it at the door, or open your tickets with one tap.",  # NEW
        "sale_eyebrow": "Purchase confirmed",  # NEW (derived)
        "hi": "Hi {n},",
        "sale_confirmed": "Your purchase for {e} is confirmed.",
        "sale_confirmed_short": "Your purchase is confirmed",
        "reference": "Reference",
        "total": "Total paid",
        "invoice": "A tax invoice (factura) for this purchase will follow in a separate email.",
        "present": "Present this reference at the event.",
        "btn_tickets": "View your tickets",  # NEW
        "paste": "Or paste this link into your browser:",  # NEW
        "link_note": "This link opens this purchase only, and stays valid until shortly after the event.",
        "outstanding": "Some of the tickets on this purchase still need answers. Open the link above to answer for your own ticket, or to enter the email address of whoever will be using each of the others.",
        "consent_h": "Confirm your optional preferences",
        "consent_p": "You, or someone using your address, ticked one or more optional boxes at checkout. We act on none of them until they are confirmed from this inbox. If that was not you, ignore this. Nothing will be sent, and nothing else will be asked.",
        "consent_link": "Confirm my preferences",  # NEW (link label)
        "presented": "Presented by",
        "sale_reason": "You're getting this because you bought tickets for {e} on Multiticketing.",  # NEW
        "digest_subject": "What's on from the things you follow",
        "digest_pre": "Ruido Blanco en vivo, Feria de Diseño Andino and more from the things you follow.",  # NEW
        "digest_intro": "Here is what's coming up from the things you follow.",
        "new": "New this week",
        "happening": "Happening this week",
        "get": "Get tickets",
        "register": "Register",
        "yours": "Your tickets",
        "going": "You're going",
        "because": "Because you follow:",
        "more": "+{c} more",
        "btn_digest": "See all upcoming events",  # NEW
        "digest_reason": "You are getting this because you follow organizers and topics on Multiticketing.",
        "unsub_q": "Don't want these?",
        "unsub_link": "Turn off the digest",
        "unsub_keep": "You'll keep everything you follow.",
        "support": "Questions? Write to",  # NEW
        "legal": "Multiticketing is operated by FUNDACION REDPLANETTRIBE, Ecuador.",  # NEW
    },
    "es": {
        "sale_subject": "Sus entradas para {e}",
        "sale_pre": "Referencia TP-7KQ2M9XA. Preséntela en la entrada o abra sus entradas con un toque.",
        "sale_eyebrow": "Compra confirmada",
        "hi": "Hola {n}:",
        "sale_confirmed": "Su compra de {e} está confirmada.",
        "sale_confirmed_short": "Su compra está confirmada",
        "reference": "Referencia",
        "total": "Total pagado",
        "invoice": "La factura de esta compra le llegará en un correo aparte.",
        "present": "Presente esta referencia en el evento.",
        "btn_tickets": "Ver sus entradas",
        "paste": "O pegue este enlace en su navegador:",
        "link_note": "Este enlace abre solo esta compra y sigue siendo válido hasta poco después del evento.",
        "outstanding": "Algunas de las entradas de esta compra aún necesitan respuestas. Abra el enlace anterior para responder por su propia entrada, o para indicar el correo electrónico de quien vaya a usar cada una de las demás.",
        "consent_h": "Confirme sus preferencias opcionales",
        "consent_p": "Usted, o alguien que usó su dirección, marcó una o más casillas opcionales al finalizar la compra. No actuamos sobre ninguna hasta que se confirme desde este buzón. Si no fue usted, ignore este mensaje: no se enviará nada ni se le volverá a preguntar.",
        "consent_link": "Confirmar mis preferencias",
        "presented": "Presentado por",
        "sale_reason": "Recibe este correo porque compró entradas para {e} en Multiticketing.",
        "digest_subject": "Novedades de lo que usted sigue",
        "digest_pre": "Ruido Blanco en vivo, Feria de Diseño Andino y más de lo que usted sigue.",
        "digest_intro": "Esto es lo que viene de las cosas que usted sigue.",
        "new": "Nuevo esta semana",
        "happening": "Esta semana",
        "get": "Consiga entradas",
        "register": "Regístrese",
        "yours": "Sus entradas",
        "going": "Va a asistir",
        "because": "Porque usted sigue:",
        "more": "+{c} más",
        "btn_digest": "Ver todos los próximos eventos",
        "digest_reason": "Usted recibe esto porque sigue organizadores y temas en Multiticketing.",
        "unsub_q": "¿No quiere recibirlos?",
        "unsub_link": "Desactive el resumen",
        "unsub_keep": "Conservará todo lo que sigue.",
        "support": "¿Preguntas? Escriba a",
        "legal": "Multiticketing es operado por FUNDACION REDPLANETTRIBE, Ecuador.",
    },
}

ORGS = {
    "altamar": ("Colectivo Altamar", "img/logo-altamar.png"),
    "ruido": ("Ruido Colectivo", "img/logo-ruido.png"),
    "andes": ("Andes Vivos", None),  # no Logo uploaded: the tile is omitted
}

SALE = dict(customer="Camila", event="Noches de Altamar", org="altamar",
            starts=dt.datetime(2026, 12, 12, 20, 0), venue="Teatro Bellavista, Quito",
            reference="TP-7KQ2M9XA", total="45.00 USD", tax_id="Cédula: 1700000017",
            cover="img/cover.jpg",
            link=f"{STOREFRONT}/tickets/confirm?token={TOKEN}")

TAGS = {"en": {"jazz": "Jazz", "rock": "Rock", "design": "Design", "film": "Film"},
        "es": {"jazz": "Jazz", "rock": "Rock", "design": "Diseño", "film": "Cine"}}

DIGEST_NEW = [
    dict(name="Ruido Blanco en vivo", starts=dt.datetime(2026, 12, 18, 21, 0), venue="Sala Mitad, Quito",
         org="ruido", thumb="img/thumb2.jpg", cta="get", because=["@org", "rock"]),
    dict(name="Feria de Diseño Andino", starts=dt.datetime(2026, 12, 20, 10, 0), venue="Galpón Norte, Quito",
         org="andes", thumb="img/thumb3.jpg", cta="register", because=["design"]),
    dict(name="Ciclo de Cine Nocturno: tres noches de cortometrajes ecuatorianos",
         starts=dt.datetime(2026, 12, 16, 19, 30), venue="Cinemateca Sur, Quito",
         org="altamar", thumb="img/thumb4.jpg", cta="get", because=["@org", "film"]),
]
DIGEST_MORE = 4
DIGEST_HAPPENING = [
    dict(name="Noches de Altamar", starts=dt.datetime(2026, 12, 12, 20, 0), venue="Teatro Bellavista, Quito",
         org="altamar", thumb="img/thumb5.jpg", cta="yours", going=True, because=["@org", "jazz"]),
    dict(name="Taller de Cerámica Abierto", starts=dt.datetime(2026, 12, 10, 18, 0), venue="Estudio Barro, Cumbayá",
         org="andes", thumb=None, cta="get", because=["@org"]),
]


def because_line(ev, loc):
    parts = [ORGS[ev["org"]][0] if b == "@org" else TAGS[loc][b] for b in ev["because"]]
    return f'{COPY[loc]["because"]} {", ".join(parts)}'


def e(s):
    return html.escape(s, quote=True)


# ---------------------------------------------------------------- shared bits
# Dark mode is a progressive enhancement for clients that honour
# prefers-color-scheme (Apple Mail, Outlook.com, some Android). Gmail ignores it
# and force-inverts instead; the Logo tile uses a background-image gradient so
# Gmail's inversion leaves it white.
DARK = """
.bg-canvas{background-color:#0b0f14!important}
.bg-card{background-color:#151b23!important}
.bg-soft{background-color:#1d2530!important}
.t-fg{color:#e8edf3!important}
.t-muted{color:#a8b2be!important}
.t-link{color:#7cc0ec!important}
.t-success{color:#6fd07c!important}
.bd{border-color:#2c3642!important}
.chip{background-color:#1d2a3d!important}
.chip-t{color:#cfe2ff!important}
.band{background-color:#1a2433!important}
.band-t{color:#cfe2ff!important}
.pill{background-color:#173a1e!important}
.tile{background-color:#ffffff!important;border-color:#ffffff!important}
"""

MOBILE = """
.wrap{width:100%!important;max-width:100%!important}
.px{padding-left:20px!important;padding-right:20px!important}
.outer{padding-left:8px!important;padding-right:8px!important}
.h1{font-size:24px!important;line-height:30px!important}
.ref{font-size:24px!important;letter-spacing:1px!important}
.hide-sm{display:none!important}
.thumb-sm{width:64px!important;height:64px!important}
"""


def doc(loc, subject, preheader, body_bg, body):
    pad = "&#847;&zwnj;&nbsp;" * 60
    return f"""<!doctype html>
<html lang="{loc}" dir="ltr" xmlns="http://www.w3.org/1999/xhtml">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="x-apple-disable-message-reformatting">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>{e(subject)}</title>
<style>
:root{{color-scheme:light dark;supported-color-schemes:light dark}}
body{{margin:0;padding:0;-webkit-text-size-adjust:100%}}
a{{text-decoration:none}}
@media (max-width:620px){{{MOBILE}}}
@media (prefers-color-scheme:dark){{{DARK}/*end-dark*/}}
</style>
</head>
<body class="bg-canvas" style="margin:0;padding:0;background-color:{body_bg};">
<div style="display:none;max-height:0;max-width:0;overflow:hidden;opacity:0;mso-hide:all;">{e(preheader)}{pad}</div>
{body}
</body>
</html>
"""


def p(text, size=16, color=FG, cls="t-fg", extra=""):
    return (f'<p class="{cls}" style="margin:0 0 16px;font-family:{FONT};font-size:{size}px;'
            f'line-height:{round(size * 1.5)}px;color:{color};{extra}">{text}</p>')


def button(label, href, align="left", full=False):
    width = ' width="100%"' if full else ""
    return f"""<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="{align}"{width} style="margin:8px 0 12px;">
<tr><td align="center" bgcolor="{PRIMARY}" style="border-radius:8px;background-color:{PRIMARY};">
<a href="{e(href)}" style="display:{'block' if full else 'inline-block'};padding:14px 28px;font-family:{FONT};font-size:16px;line-height:20px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:8px;">{e(label)}</a>
</td></tr></table>"""


def fallback(loc, href, align="left"):
    return (f'<p class="t-muted" style="clear:both;margin:0 0 16px;font-family:{FONT};font-size:13px;line-height:19px;'
            f'color:{MUTED};text-align:{align};">{COPY[loc]["paste"]}<br>'
            f'<a class="t-link" href="{e(href)}" style="color:{PRIMARY};word-break:break-all;text-decoration:underline;">{e(href)}</a></p>')


def org_row(loc, org_key, size=28, prefix=True, fsize=15):
    name, logo = ORGS[org_key]
    label = f'{COPY[loc]["presented"]} <strong class="t-fg" style="color:{FG};font-weight:600;">{e(name)}</strong>' \
        if prefix else f'<span class="t-fg" style="color:{FG};font-weight:600;">{e(name)}</span>'
    tile = ""
    if logo:
        inner = size - 8
        tile = (f'<td class="tile" width="{size}" height="{size}" valign="middle" align="center" '
                f'style="width:{size}px;height:{size}px;background-color:#ffffff;background-image:linear-gradient(#ffffff,#ffffff);'
                f'border:1px solid {BORDER};border-radius:6px;">'
                f'<img src="{logo}" width="{inner}" height="{inner}" alt="" style="display:block;margin:0 auto;border:0;width:{inner}px;height:{inner}px;"></td>'
                f'<td width="8" style="width:8px;font-size:0;line-height:0;">&nbsp;</td>')
    return (f'<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>{tile}'
            f'<td class="t-muted" valign="middle" style="font-family:{FONT};font-size:{fsize}px;line-height:20px;color:{MUTED};">{label}</td>'
            f'</tr></table>')


def cta(ev, loc):
    key = ev["cta"]
    return COPY[loc][key], f"{STOREFRONT}/{loc}/e/{ev['name'][:12].lower().replace(' ', '-')}"


def wordmark(size=28, text=16, color=FG):
    return (f'<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>'
            f'<td valign="middle"><img src="img/mark.png" width="{size}" height="{size}" alt="" '
            f'style="display:block;border:0;border-radius:{max(4, size // 5)}px;width:{size}px;height:{size}px;"></td>'
            f'<td valign="middle" class="t-fg" style="padding-left:8px;font-family:{FONT};font-size:{text}px;line-height:20px;'
            f'font-weight:700;letter-spacing:-0.2px;color:{color};">Multiticketing</td></tr></table>')


def footer(loc, reason, unsub=None, align="center"):
    c = COPY[loc]
    s = f"margin:0 0 8px;font-family:{FONT};font-size:13px;line-height:19px;color:{MUTED};text-align:{align};"
    out = f'<p class="t-muted" style="{s}">{reason}</p>'
    if unsub:
        out += (f'<p class="t-muted" style="{s}">{c["unsub_q"]} <a class="t-fg" href="{unsub}" '
                f'style="color:{FG};text-decoration:underline;">{c["unsub_link"]}</a>. {c["unsub_keep"]}</p>')
    out += (f'<p class="t-muted" style="{s}">{c["support"]} <a class="t-fg" href="mailto:info@redplanettribe.org" '
            f'style="color:{FG};text-decoration:underline;">info@redplanettribe.org</a></p>')
    out += f'<p class="t-muted" style="{s}">{c["legal"]}</p>'
    return out


def outer(inner, width=600, bg=CANVAS, pad_y=32):
    return f"""<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="bg-canvas" style="background-color:{bg};">
<tr><td align="center" class="outer" style="padding:{pad_y}px 12px;">
<!--[if mso]><table role="presentation" width="{width}" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->
<table role="presentation" class="wrap" width="{width}" cellpadding="0" cellspacing="0" border="0" style="width:{width}px;max-width:{width}px;">
{inner}
</table>
<!--[if mso]></td></tr></table><![endif]-->
</td></tr></table>"""


def hr(margin="24px 0"):
    return f'<div class="bd" style="height:0;border-top:1px solid {BORDER};margin:{margin};font-size:0;line-height:0;">&nbsp;</div>'


def link(label, href, size=15):
    return (f'<a class="t-link" href="{e(href)}" style="font-family:{FONT};font-size:{size}px;font-weight:600;'
            f'color:{PRIMARY};text-decoration:none;">{e(label)}&nbsp;&rarr;</a>')


def cover_img(radius="0", width=600):
    return (f'<img src="{SALE["cover"]}" width="{width}" alt="" '
            f'style="display:block;width:100%;max-width:{width}px;height:auto;border:0;border-radius:{radius};">')


UNSUB = f"{STOREFRONT}/unsubscribe?token=dGhpcy1pcy1hLXByb3RvdHlwZQ"
MORE_URL = f"{STOREFRONT}/following"
ALL_URL = f"{STOREFRONT}/"


def going_pill(loc, size=13):
    return (f'<span class="pill t-success" style="display:inline-block;padding:2px 8px;border-radius:999px;'
            f'background-color:#e3f4e6;color:{SUCCESS};font-family:{FONT};font-size:{size}px;line-height:18px;'
            f'font-weight:600;">&#10003;&nbsp;{COPY[loc]["going"]}</span>')


# ================================================================= A: Card
# Mirrors the Storefront's Event card: grey canvas, one white rounded card,
# Cover Image full-bleed across its top, details in a soft box. Digest rows are
# the Timeline card: text left, square Thumbnail right.

def a_sale(loc):
    c, s = COPY[loc], SALE
    meta = f"margin:0;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};"
    body = f"""
<tr><td class="px" style="padding:0 4px 16px;">{wordmark()}</td></tr>
<tr><td class="bg-card bd" style="background-color:#ffffff;border:1px solid {BORDER};border-radius:12px;overflow:hidden;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
<tr><td style="font-size:0;line-height:0;">{cover_img("12px 12px 0 0")}</td></tr>
<tr><td class="px" style="padding:28px 32px 8px;">
<p class="t-success" style="margin:0 0 8px;font-family:{FONT};font-size:14px;line-height:20px;font-weight:600;color:{SUCCESS};">&#10003;&nbsp;{c["sale_eyebrow"]}</p>
<h1 class="t-fg h1" style="margin:0 0 8px;font-family:{FONT};font-size:28px;line-height:34px;font-weight:700;letter-spacing:-0.4px;color:{FG};">{e(s["event"])}</h1>
<p class="t-muted" style="{meta}">{when(s["starts"], loc)}</p>
<p class="t-muted" style="{meta}margin-bottom:14px;">{e(s["venue"])}</p>
{org_row(loc, s["org"])}
{hr("24px 0")}
{p(c["hi"].format(n=s["customer"]))}
{p(c["sale_confirmed"].format(e="<strong>" + e(s["event"]) + "</strong>"))}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="bg-soft" style="background-color:{CANVAS};border-radius:10px;margin:0 0 16px;">
<tr><td style="padding:18px 20px;">
<p class="t-muted" style="margin:0 0 2px;font-family:{FONT};font-size:13px;line-height:18px;color:{MUTED};">{c["reference"]}</p>
<p class="t-fg ref" style="margin:0 0 14px;font-family:{MONO};font-size:26px;line-height:32px;font-weight:700;letter-spacing:2px;color:{FG};">{s["reference"]}</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td valign="top" width="50%" style="padding-right:8px;">
<p class="t-muted" style="margin:0 0 2px;font-family:{FONT};font-size:13px;line-height:18px;color:{MUTED};">{c["total"]}</p>
<p class="t-fg" style="margin:0;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{s["total"]}</p></td>
<td valign="top" width="50%">
<p class="t-muted" style="margin:0 0 2px;font-family:{FONT};font-size:13px;line-height:18px;color:{MUTED};">{s["tax_id"].split(":")[0]}</p>
<p class="t-fg" style="margin:0;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{s["tax_id"].split(": ")[1]}</p></td>
</tr></table>
</td></tr></table>
{p(c["present"])}
{button(c["btn_tickets"], s["link"], full=True)}
{fallback(loc, s["link"])}
{p(c["link_note"], 15, MUTED, "t-muted")}
{p(c["invoice"], 15, MUTED, "t-muted")}
{p(c["outstanding"])}
</td></tr>
<tr><td class="px bd" style="padding:20px 32px 12px;border-top:1px solid {BORDER};">
<p class="t-fg" style="margin:0 0 6px;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{c["consent_h"]}</p>
{p(c["consent_p"], 15, MUTED, "t-muted", "margin-bottom:10px;")}
<p style="margin:0 0 12px;">{link(c["consent_link"], STOREFRONT + "/consent/confirm?token=abc")}</p>
</td></tr>
</table>
</td></tr>
<tr><td class="px" style="padding:24px 16px 0;">{footer(loc, c["sale_reason"].format(e=e(s["event"])))}</td></tr>
"""
    return doc(loc, c["sale_subject"].format(e=s["event"]), c["sale_pre"], CANVAS, outer(body))


def a_row(ev, loc, last):
    label, href = cta(ev, loc)
    meta = f"margin:0;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};"
    thumb = ""
    if ev["thumb"]:
        thumb = (f'<td width="96" valign="top" align="right" style="width:96px;padding-left:16px;">'
                 f'<img class="thumb-sm" src="{ev["thumb"]}" width="88" height="88" alt="" '
                 f'style="display:block;width:88px;height:88px;border:0;border-radius:8px;"></td>')
    border = "" if last else f"border-bottom:1px solid {BORDER};"
    going = f'<p style="margin:0 0 6px;">{going_pill(loc)}</p>' if ev.get("going") else ""
    return f"""<tr><td class="bd" style="padding:18px 0;{border}">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td valign="top">
{going}<p class="t-muted" style="{meta}">{when(ev["starts"], loc)}</p>
<p class="t-fg" style="margin:2px 0 2px;font-family:{FONT};font-size:17px;line-height:23px;font-weight:600;color:{FG};">{e(ev["name"])}</p>
<p class="t-muted" style="{meta}margin-bottom:8px;">{e(ev["venue"])}</p>
{org_row(loc, ev["org"], 24, False, 14)}
<p style="margin:10px 0 4px;">{link(label, href)}</p>
<p class="t-muted" style="margin:0;font-family:{FONT};font-size:13px;line-height:18px;color:{MUTED};">{e(because_line(ev, loc))}</p>
</td>{thumb}
</tr></table>
</td></tr>"""


def a_section(title, events, loc, more=0):
    rows = "".join(a_row(ev, loc, i == len(events) - 1 and not more) for i, ev in enumerate(events))
    more_row = ""
    if more:
        more_row = f'<tr><td class="bd" style="padding:14px 0 4px;border-top:1px solid {BORDER};">{link(COPY[loc]["more"].format(c=more), MORE_URL)}</td></tr>'
    return f"""<tr><td style="padding:24px 0 0;">
<p class="t-muted" style="margin:0;font-family:{FONT};font-size:13px;line-height:18px;font-weight:700;letter-spacing:1px;text-transform:uppercase;color:{MUTED};">{title}</p>
</td></tr>{rows}{more_row}"""


def a_digest(loc):
    c = COPY[loc]
    body = f"""
<tr><td class="px" style="padding:0 4px 16px;">{wordmark()}</td></tr>
<tr><td class="bg-card bd px" style="background-color:#ffffff;border:1px solid {BORDER};border-radius:12px;padding:32px 32px 28px;">
<h1 class="t-fg h1" style="margin:0 0 12px;font-family:{FONT};font-size:26px;line-height:32px;font-weight:700;letter-spacing:-0.4px;color:{FG};">{c["digest_subject"]}</h1>
{p(c["hi"].format(n="Camila") + "<br>" + c["digest_intro"], extra="margin-bottom:0;")}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
{a_section(c["new"], DIGEST_NEW, loc, DIGEST_MORE)}
{a_section(c["happening"], DIGEST_HAPPENING, loc)}
</table>
<div style="height:16px;line-height:16px;font-size:0;">&nbsp;</div>
{button(c["btn_digest"], ALL_URL, full=True)}
{fallback(loc, ALL_URL)}
</td></tr>
<tr><td class="px" style="padding:24px 16px 0;">{footer(loc, c["digest_reason"], UNSUB)}</td></tr>
"""
    return doc(loc, c["digest_subject"], c["digest_pre"], CANVAS, outer(body))


# ================================================================= B: Ticket
# A white page with one object on it: the purchase drawn as a ticket, Cover
# Image on top, a perforation, and a stub holding the Reference. Centred and
# celebratory. Digest rows lead with a date chip, like the Explorer timeline's
# day buckets, and sections sit on tinted bands.

def b_sale(loc):
    c, s = COPY[loc], SALE
    BG = "#fbfcfd"
    centered = f"font-family:{FONT};text-align:center;"
    meta = f"margin:0;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};"
    label = f"margin:0 0 4px;font-family:{FONT};font-size:12px;line-height:16px;font-weight:700;letter-spacing:1.2px;text-transform:uppercase;color:{MUTED};"
    body = f"""
<tr><td align="center" style="padding:0 0 28px;"><table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td>{wordmark(26, 15)}</td></tr></table></td></tr>
<tr><td class="px" style="padding:0 24px 8px;{centered}">
<h1 class="t-fg h1" style="margin:0 0 10px;font-family:{FONT};font-size:30px;line-height:36px;font-weight:800;letter-spacing:-0.6px;color:{FG};">{c["sale_subject"].format(e=e(s["event"]))}</h1>
<p class="t-muted" style="margin:0 0 28px;font-family:{FONT};font-size:17px;line-height:25px;color:{MUTED};">{c["hi"].format(n=s["customer"])}<br>{c["sale_confirmed"].format(e=e(s["event"]))}</p>
</td></tr>
<tr><td class="px" style="padding:0 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="bg-card bd" style="background-color:#ffffff;border:1px solid {BORDER};border-radius:16px;">
<tr><td style="padding:10px 10px 0;font-size:0;line-height:0;">{cover_img("10px", 548)}</td></tr>
<tr><td style="padding:20px 24px 22px;">
<p class="t-fg" style="margin:0 0 6px;font-family:{FONT};font-size:21px;line-height:27px;font-weight:700;color:{FG};">{e(s["event"])}</p>
<p class="t-muted" style="{meta}">{when(s["starts"], loc)}</p>
<p class="t-muted" style="{meta}margin-bottom:14px;">{e(s["venue"])}</p>
{org_row(loc, s["org"])}
</td></tr>
<tr><td style="padding:0 12px;"><div class="bd" style="height:0;border-top:2px dashed {BORDER};font-size:0;line-height:0;">&nbsp;</div></td></tr>
<tr><td style="padding:22px 24px 8px;" align="center">
<p class="t-muted" style="{label}text-align:center;">{c["reference"]}</p>
<p class="t-fg ref" style="margin:0 0 6px;font-family:{MONO};font-size:34px;line-height:42px;font-weight:700;letter-spacing:3px;color:{FG};text-align:center;">{s["reference"]}</p>
<p class="t-muted" style="margin:0 0 18px;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};text-align:center;">{c["present"]}</p>
</td></tr>
<tr><td style="padding:0 24px 22px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="bd" style="border-top:1px solid {BORDER};"><tr>
<td valign="top" width="50%" style="padding:16px 8px 0 0;"><p class="t-muted" style="{label}">{c["total"]}</p>
<p class="t-fg" style="margin:0;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{s["total"]}</p></td>
<td valign="top" width="50%" style="padding:16px 0 0 8px;"><p class="t-muted" style="{label}">{s["tax_id"].split(":")[0]}</p>
<p class="t-fg" style="margin:0;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{s["tax_id"].split(": ")[1]}</p></td>
</tr></table>
</td></tr>
</table>
</td></tr>
<tr><td class="px" align="center" style="padding:28px 40px 0;">
<table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td>{button(c["btn_tickets"], s["link"], "center")}</td></tr></table>
{fallback(loc, s["link"], "center")}
</td></tr>
<tr><td class="px" style="padding:8px 40px 0;">
{p(c["link_note"], 15, MUTED, "t-muted", "text-align:center;")}
{p(c["invoice"], 15, MUTED, "t-muted", "text-align:center;")}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="band" style="background-color:{ACCENT};border-radius:10px;margin:8px 0 24px;">
<tr><td style="padding:16px 18px;"><p class="band-t" style="margin:0;font-family:{FONT};font-size:16px;line-height:24px;color:{ACCENT_FG};">{c["outstanding"]}</p></td></tr></table>
{hr("0 0 20px")}
<p class="t-fg" style="margin:0 0 6px;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{c["consent_h"]}</p>
{p(c["consent_p"], 15, MUTED, "t-muted", "margin-bottom:10px;")}
<p style="margin:0 0 8px;">{link(c["consent_link"], STOREFRONT + "/consent/confirm?token=abc")}</p>
{hr("20px 0 20px")}
{footer(loc, c["sale_reason"].format(e=e(s["event"])))}
</td></tr>
"""
    return doc(loc, c["sale_subject"].format(e=s["event"]), c["sale_pre"], BG, outer(body, bg=BG, pad_y=36))


def b_row(ev, loc):
    label, href = cta(ev, loc)
    day, mon = chip(ev["starts"], loc)
    time = ev["starts"].strftime("%H:%M")
    thumb = ""
    if ev["thumb"]:
        thumb = (f'<td width="80" valign="top" align="right" class="hide-sm" style="width:80px;padding-left:14px;">'
                 f'<img src="{ev["thumb"]}" width="72" height="72" alt="" style="display:block;width:72px;height:72px;border:0;border-radius:10px;"></td>')
    going = f'<p style="margin:6px 0 0;">{going_pill(loc)}</p>' if ev.get("going") else ""
    return f"""<tr><td style="padding:16px 0;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td width="58" valign="top" style="width:58px;">
<table role="presentation" width="58" cellpadding="0" cellspacing="0" border="0" class="chip" style="width:58px;background-color:{ACCENT};border-radius:10px;">
<tr><td align="center" style="padding:8px 0 2px;font-family:{FONT};font-size:24px;line-height:26px;font-weight:800;color:{PRIMARY};" class="chip-t">{day}</td></tr>
<tr><td align="center" style="padding:0 0 8px;font-family:{FONT};font-size:12px;line-height:14px;font-weight:700;letter-spacing:1px;color:{ACCENT_FG};" class="chip-t">{mon}</td></tr>
</table></td>
<td valign="top" style="padding-left:14px;">
<p class="t-fg" style="margin:0 0 2px;font-family:{FONT};font-size:17px;line-height:23px;font-weight:700;color:{FG};">{e(ev["name"])}</p>
<p class="t-muted" style="margin:0 0 8px;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};">{time} &middot; {e(ev["venue"])}</p>
{org_row(loc, ev["org"], 24, False, 14)}
{going}
<p style="margin:10px 0 2px;">{link(label, href)}</p>
<p class="t-muted" style="margin:0;font-family:{FONT};font-size:13px;line-height:18px;color:{MUTED};">{e(because_line(ev, loc))}</p>
</td>{thumb}
</tr></table></td></tr>"""


def b_section(title, events, loc, more=0):
    rows = "".join(b_row(ev, loc) for ev in sorted(events, key=lambda x: x["starts"]))
    more_row = f'<tr><td style="padding:4px 0 8px 72px;">{link(COPY[loc]["more"].format(c=more), MORE_URL)}</td></tr>' if more else ""
    return f"""<tr><td style="padding:24px 0 4px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="band" style="background-color:{ACCENT};border-radius:8px;">
<tr><td class="band-t" style="padding:10px 14px;font-family:{FONT};font-size:15px;line-height:20px;font-weight:700;color:{ACCENT_FG};">{title}</td></tr></table>
</td></tr>{rows}{more_row}"""


def b_digest(loc):
    c = COPY[loc]
    BG = "#fbfcfd"
    body = f"""
<tr><td align="center" style="padding:0 0 28px;"><table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td>{wordmark(26, 15)}</td></tr></table></td></tr>
<tr><td class="px" style="padding:0 24px;text-align:center;">
<h1 class="t-fg h1" style="margin:0 0 10px;font-family:{FONT};font-size:30px;line-height:36px;font-weight:800;letter-spacing:-0.6px;color:{FG};">{c["digest_subject"]}</h1>
<p class="t-muted" style="margin:0 0 8px;font-family:{FONT};font-size:17px;line-height:25px;color:{MUTED};">{c["hi"].format(n="Camila")}<br>{c["digest_intro"]}</p>
</td></tr>
<tr><td class="px" style="padding:0 24px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
{b_section(c["new"], DIGEST_NEW, loc, DIGEST_MORE)}
{b_section(c["happening"], DIGEST_HAPPENING, loc)}
</table></td></tr>
<tr><td class="px" align="center" style="padding:24px 40px 0;">
<table role="presentation" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td>{button(c["btn_digest"], ALL_URL, "center")}</td></tr></table>
{fallback(loc, ALL_URL, "center")}
{hr("12px 0 20px")}
{footer(loc, c["digest_reason"], UNSUB)}
</td></tr>
"""
    return doc(loc, c["digest_subject"], c["digest_pre"], BG, outer(body, bg=BG, pad_y=36))


# ================================================================= C: Letter
# No canvas and no card: a narrow, left-aligned column that reads like a note
# from the platform. Text first, the facts as hairline rows, the Event (with
# its Cover Image) as the closing block. Digest is a dense list with the
# Thumbnail on the left.

def c_kv(label, value, mono=False, last=False):
    font = MONO if mono else FONT
    border = "" if last else f"border-bottom:1px solid {BORDER};"
    size = "18px;letter-spacing:1px" if mono else "16px"
    return (f'<tr><td class="t-muted bd" style="padding:12px 0;{border}font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};">{label}</td>'
            f'<td class="t-fg bd" align="right" style="padding:12px 0;{border}font-family:{font};font-size:{size};line-height:22px;font-weight:700;color:{FG};">{value}</td></tr>')


def c_header():
    return f'<tr><td class="px" style="padding:0 0 20px;">{wordmark(22, 15)}</td></tr><tr><td class="px" style="padding:0;">{hr("0 0 32px")}</td></tr>'


def c_sale(loc):
    c, s = COPY[loc], SALE
    meta = f"margin:0;font-family:{FONT};font-size:15px;line-height:22px;color:{MUTED};"
    body = f"""
{c_header()}
<tr><td class="px" style="padding:0;">
<h1 class="t-fg h1" style="margin:0 0 20px;font-family:{FONT};font-size:26px;line-height:32px;font-weight:700;letter-spacing:-0.4px;color:{FG};">{c["sale_confirmed_short"]}</h1>
{p(c["hi"].format(n=s["customer"]))}
{p(c["sale_confirmed"].format(e="<strong>" + e(s["event"]) + "</strong>"))}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="bd" style="border-top:2px solid {FG};margin:8px 0 16px;">
{c_kv(c["reference"], s["reference"], mono=True)}
{c_kv(c["total"], s["total"])}
{c_kv(s["tax_id"].split(":")[0], s["tax_id"].split(": ")[1], last=True)}
</table>
{p(c["present"])}
{button(c["btn_tickets"], s["link"])}
{fallback(loc, s["link"])}
{p(c["link_note"], 15, MUTED, "t-muted")}
{p(c["outstanding"])}
{p(c["invoice"], 15, MUTED, "t-muted")}
</td></tr>
<tr><td class="px" style="padding:12px 0 0;">
<div style="font-size:0;line-height:0;">{cover_img("8px", 560)}</div>
<p class="t-fg" style="margin:16px 0 4px;font-family:{FONT};font-size:19px;line-height:25px;font-weight:700;color:{FG};">{e(s["event"])}</p>
<p class="t-muted" style="{meta}">{when(s["starts"], loc)} &middot; {e(s["venue"])}</p>
<div style="height:12px;line-height:12px;font-size:0;">&nbsp;</div>
{org_row(loc, s["org"])}
{hr("32px 0 20px")}
<p class="t-fg" style="margin:0 0 6px;font-family:{FONT};font-size:16px;line-height:22px;font-weight:600;color:{FG};">{c["consent_h"]}</p>
{p(c["consent_p"], 15, MUTED, "t-muted", "margin-bottom:10px;")}
<p style="margin:0 0 8px;">{link(c["consent_link"], STOREFRONT + "/consent/confirm?token=abc")}</p>
{hr("20px 0 20px")}
{footer(loc, c["sale_reason"].format(e=e(s["event"])), align="left")}
</td></tr>
"""
    return doc(loc, c["sale_subject"].format(e=s["event"]), c["sale_pre"], "#ffffff",
               outer(body, 560, "#ffffff", 36))


def c_row(ev, loc):
    label, href = cta(ev, loc)
    thumb = ""
    pad = ""
    if ev["thumb"]:
        thumb = (f'<td width="64" valign="top" style="width:64px;padding-right:14px;">'
                 f'<img src="{ev["thumb"]}" width="64" height="64" alt="" style="display:block;width:64px;height:64px;border:0;border-radius:6px;"></td>')
    going = f'{going_pill(loc, 12)}&nbsp; ' if ev.get("going") else ""
    return f"""<tr><td class="bd" style="padding:14px 0;border-bottom:1px solid {BORDER};">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>{thumb}
<td valign="top" style="{pad}">
<p class="t-fg" style="margin:0 0 2px;font-family:{FONT};font-size:16px;line-height:22px;font-weight:700;color:{FG};">{e(ev["name"])}</p>
<p class="t-muted" style="margin:0 0 6px;font-family:{FONT};font-size:15px;line-height:21px;color:{MUTED};">{when(ev["starts"], loc)} &middot; {e(ev["venue"])}</p>
{org_row(loc, ev["org"], 20, False, 14)}
<p style="margin:8px 0 0;font-family:{FONT};font-size:13px;line-height:20px;">{going}{link(label, href, 14)}<span class="t-muted" style="color:{MUTED};"> &nbsp;&middot;&nbsp; {e(because_line(ev, loc))}</span></p>
</td></tr></table></td></tr>"""


def c_section(title, events, loc, more=0):
    rows = "".join(c_row(ev, loc) for ev in events)
    more_row = f'<tr><td style="padding:12px 0 0;">{link(COPY[loc]["more"].format(c=more), MORE_URL)}</td></tr>' if more else ""
    return f"""<tr><td class="bd" style="padding:28px 0 2px;border-bottom:2px solid {FG};">
<h2 class="t-fg" style="margin:0 0 8px;font-family:{FONT};font-size:18px;line-height:24px;font-weight:700;color:{FG};">{title}</h2></td></tr>{rows}{more_row}"""


def c_digest(loc):
    c = COPY[loc]
    body = f"""
{c_header()}
<tr><td class="px" style="padding:0;">
<h1 class="t-fg h1" style="margin:0 0 16px;font-family:{FONT};font-size:26px;line-height:32px;font-weight:700;letter-spacing:-0.4px;color:{FG};">{c["digest_subject"]}</h1>
{p(c["hi"].format(n="Camila") + "<br>" + c["digest_intro"], extra="margin-bottom:0;")}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
{c_section(c["new"], DIGEST_NEW, loc, DIGEST_MORE)}
{c_section(c["happening"], DIGEST_HAPPENING, loc)}
</table>
<div style="height:24px;line-height:24px;font-size:0;">&nbsp;</div>
{button(c["btn_digest"], ALL_URL)}
{fallback(loc, ALL_URL)}
{hr("20px 0 20px")}
{footer(loc, c["digest_reason"], UNSUB, align="left")}
</td></tr>
"""
    return doc(loc, c["digest_subject"], c["digest_pre"], "#ffffff", outer(body, 560, "#ffffff", 36))


DIRECTIONS = {"A": (a_sale, a_digest), "B": (b_sale, b_digest), "C": (c_sale, c_digest)}

if __name__ == "__main__":
    if os.path.isdir(DIST):
        shutil.rmtree(DIST)
    os.makedirs(DIST)
    shutil.copytree(os.path.join(HERE, "img"), os.path.join(DIST, "img"))
    shutil.copy(os.path.join(HERE, "viewer.html"), os.path.join(DIST, "index.html"))
    for key, (sale, digest) in DIRECTIONS.items():
        for loc in ("en", "es"):
            for kind, fn in (("sale", sale), ("digest", digest)):
                with open(os.path.join(DIST, f"{key}-{kind}-{loc}.html"), "w") as f:
                    f.write(fn(loc))
    print("built", len(os.listdir(DIST)) - 2, "mails into", DIST)
