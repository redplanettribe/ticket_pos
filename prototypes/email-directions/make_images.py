# PROTOTYPE, throwaway: invented sample imagery for the email direction prototype.
# No text is baked into any image.
import math
import os
import random

from PIL import Image, ImageDraw, ImageFilter

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "img")
os.makedirs(OUT, exist_ok=True)


def grad(w, h, c1, c2, angle=0.8):
    im = Image.new("RGB", (w, h))
    px = im.load()
    ca, sa = math.cos(angle), math.sin(angle)
    for y in range(h):
        for x in range(w):
            t = max(0, min(1, ((x / w) * ca + (y / h) * sa + 0.2) / 1.6))
            px[x, y] = tuple(int(c1[i] + (c2[i] - c1[i]) * t) for i in range(3))
    return im


def scene(w, h, c1, c2, blobs, seed):
    random.seed(seed)
    im = grad(w, h, c1, c2)
    d = ImageDraw.Draw(im, "RGBA")
    for col in blobs:
        r = random.randint(h // 5, h // 2)
        x, y = random.randint(0, w), random.randint(0, h // 2)
        d.ellipse([x - r, y - r, x + r, y + r], fill=col)
    im = im.filter(ImageFilter.GaussianBlur(w / 40))
    d = ImageDraw.Draw(im, "RGBA")
    for i in range(15):
        x = int(w * i / 14)
        hh = random.randint(h // 7, h // 4)
        d.ellipse([x - w // 20, h - hh, x + w // 20, h + hh], fill=(10, 12, 25, 235))
    return im


scene(1200, 675, (18, 24, 70), (214, 92, 58),
      [(255, 190, 90, 150), (90, 160, 255, 120), (255, 90, 160, 110)], 1).save(
    os.path.join(OUT, "cover.jpg"), quality=85)

specs = [((20, 60, 90), (80, 200, 190), 2), ((60, 20, 70), (240, 120, 80), 3),
         ((15, 40, 30), (170, 210, 90), 4), ((30, 30, 60), (130, 140, 250), 5),
         ((80, 40, 20), (250, 200, 110), 6)]
for i, (a, b, s) in enumerate(specs):
    scene(320, 320, a, b, [(255, 255, 255, 90), (255, 200, 120, 100)], s).resize(
        (160, 160), Image.LANCZOS).save(os.path.join(OUT, f"thumb{i + 1}.jpg"), quality=85)


def logo(name, fn):
    im = Image.new("RGBA", (240, 240), (0, 0, 0, 0))
    fn(ImageDraw.Draw(im))
    im.save(os.path.join(OUT, f"{name}.png"))


def altamar(d):
    d.polygon([(30, 190), (120, 40), (210, 190)], fill=(12, 74, 110))
    d.rectangle([30, 198, 210, 212], fill=(240, 140, 40))


def ruido(d):
    d.ellipse([30, 30, 210, 210], fill=(20, 20, 20))
    d.ellipse([85, 85, 155, 155], fill=(230, 60, 90))


def andes(d):
    for i, h in enumerate([60, 120, 170, 100]):
        d.rectangle([40 + i * 42, 200 - h, 72 + i * 42, 200], fill=(40, 120, 60))


logo("logo-altamar", altamar)
logo("logo-ruido", ruido)
logo("logo-andes", andes)
