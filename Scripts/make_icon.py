# Renders the app icon: python3 Scripts/make_icon.py ios/Icon.png desktop/assets/icon.png
# (1024 px for iOS, 256 px for the desktop companion's notifications).
import math, sys
from PIL import Image, ImageDraw, ImageFilter

S = 2048  # supersample
img = Image.new("RGB", (S, S))
px = img.load()
# Diagonal gradient: warm coral (top left) to deep magenta (bottom right).
a, b = (255, 122, 89), (190, 40, 120)
for y in range(S):
    for x in range(0, S, 4):
        t = (x * 0.6 + y) / (1.6 * S)
        c = tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))
        for dx in range(4):
            px[x + dx, y] = c
d = ImageDraw.Draw(img)

def soft_shadow(shape_fn, offset=(0, 30), blur=0.02, alpha=110):
    m = Image.new("L", (S, S), 0)
    shape_fn(ImageDraw.Draw(m), offset)
    img.paste((60, 10, 40), (0, 0), m.filter(ImageFilter.GaussianBlur(S * blur)).point(lambda v: v * alpha // 255))

# Laptop (right, low).
lx0, ly0, lx1, ly1 = S * 0.47, S * 0.52, S * 0.90, S * 0.80
def laptop(dr, o):
    dr.rounded_rectangle([lx0 + o[0], ly0 + o[1], lx1 + o[0], ly1 + o[1]], radius=S * 0.025, fill=255)
    dr.polygon([(lx0 - S * 0.04 + o[0], ly1 + o[1]), (lx1 + S * 0.04 + o[0], ly1 + o[1]),
                (lx1 + o[0], ly1 + S * 0.045 + o[1]), (lx0 + o[0], ly1 + S * 0.045 + o[1])], fill=255)
soft_shadow(laptop)
d.rounded_rectangle([lx0, ly0, lx1, ly1], radius=S * 0.025, fill=(250, 245, 250))
d.rounded_rectangle([lx0 + S * 0.02, ly0 + S * 0.02, lx1 - S * 0.02, ly1 - S * 0.02], radius=S * 0.01, fill=(70, 20, 60))
d.polygon([(lx0 - S * 0.04, ly1), (lx1 + S * 0.04, ly1), (lx1, ly1 + S * 0.045), (lx0, ly1 + S * 0.045)], fill=(235, 215, 235))
# A "received" card on the laptop screen.
d.rounded_rectangle([lx0 + S * 0.22, ly0 + S * 0.05, lx1 - S * 0.045, ly0 + S * 0.13], radius=S * 0.015, fill=(255, 200, 120))

# Phone (left).
px0, py0, px1, py1 = S * 0.10, S * 0.40, S * 0.36, S * 0.86
def phone(dr, o):
    dr.rounded_rectangle([px0 + o[0], py0 + o[1], px1 + o[0], py1 + o[1]], radius=S * 0.045, fill=255)
soft_shadow(phone)
d.rounded_rectangle([px0, py0, px1, py1], radius=S * 0.045, fill=(250, 245, 250))
d.rounded_rectangle([px0 + S * 0.018, py0 + S * 0.018, px1 - S * 0.018, py1 - S * 0.018], radius=S * 0.03, fill=(70, 20, 60))
d.rounded_rectangle([(px0 + px1) / 2 - S * 0.035, py0 + S * 0.032, (px0 + px1) / 2 + S * 0.035, py0 + S * 0.054], radius=S * 0.011, fill=(20, 5, 20))

# Dotted arc from the phone up and over to the laptop.
cx, cy, r = S * 0.50, S * 0.50, S * 0.27
for deg in range(200, 301, 9):
    t = math.radians(deg)
    x, y = cx + r * math.cos(t), cy + r * 0.9 * math.sin(t)
    rr = S * 0.012
    d.ellipse([x - rr, y - rr, x + rr, y + rr], fill=(255, 255, 255))

# Paper plane at the end of the arc, flying down-right.
plane = [(0, 0), (-1.0, -0.42), (-0.62, 0.02), (-1.0, 0.42)]
ang = math.radians(42)
pxc, pyc, sc = S * 0.775, S * 0.395, S * 0.13
pts = [(pxc + sc * (x * math.cos(ang) - y * math.sin(ang)), pyc + sc * (x * math.sin(ang) + y * math.cos(ang))) for x, y in plane]
def plane_shape(dr, o):
    dr.polygon([(x + o[0], y + o[1]) for x, y in pts], fill=255)
soft_shadow(plane_shape, offset=(0, 24), blur=0.012)
d.polygon(pts, fill=(255, 255, 255))
d.polygon([pts[0], pts[2], pts[3]], fill=(240, 225, 240))
d.line([pts[0], pts[2]], fill=(220, 190, 220), width=int(S * 0.006))

big = img.resize((1024, 1024), Image.LANCZOS)
big.save(sys.argv[1], optimize=True)
for out in sys.argv[2:]:
    img.resize((256, 256), Image.LANCZOS).save(out, optimize=True)
