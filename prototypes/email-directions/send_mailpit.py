# PROTOTYPE, throwaway: deliver every built direction into a local Mailpit as a
# multipart mail (HTML + a stand-in plain-text part). Images point at the local
# static server, standing in for the public media bucket.
#   python3 -I send_mailpit.py [smtp_port] [image_base_url]
import os
import re
import smtplib
import sys
from email.message import EmailMessage

HERE = os.path.dirname(os.path.abspath(__file__))
DIST = os.path.join(HERE, "dist")
port = int(sys.argv[1]) if len(sys.argv) > 1 else 64025
base = sys.argv[2] if len(sys.argv) > 2 else "http://localhost:64390"

with smtplib.SMTP("127.0.0.1", port) as smtp:
    for name in sorted(os.listdir(DIST)):
        if not re.match(r"[ABC]-(sale|digest)-(en|es)\.html$", name):
            continue
        html = open(os.path.join(DIST, name)).read().replace('src="img/', f'src="{base}/img/')
        subject = re.search(r"<title>(.*?)</title>", html).group(1)
        d, kind, loc = name[:-5].split("-")
        msg = EmailMessage()
        msg["From"] = "Multiticketing <no-reply@multiticketing.com>"
        msg["To"] = "Camila <camila@example.com>"
        msg["Subject"] = f"[{d} {loc.upper()}] {subject}".replace("&#x27;", "'")
        msg.set_content("(prototype: the production plain-text part goes here, unchanged)")
        msg.add_alternative(html, subtype="html")
        smtp.send_message(msg)
        print("sent", name)
