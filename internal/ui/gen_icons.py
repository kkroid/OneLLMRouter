"""Generate tray icons for OneLLMRouter.

Design: a status-colored rounded badge with a white hexagon glyph.
"""
import math
import struct

from PIL import Image, ImageDraw

MASTER_SIZE = 256
ICON_SIZE = 16


def make_bmp_resource(img_16):
    w, h = img_16.size
    bih = struct.pack("<IiiHHIIiiII",
                      40, w, h * 2, 1, 32, 0, w * h * 4, 0, 0, 0, 0)

    xor = bytearray()
    pixels = img_16.load()
    for y in range(h - 1, -1, -1):
        for x in range(w):
            r, g, b, a = pixels[x, y]
            xor.extend([b, g, r, a])

    row_bytes = ((w + 31) // 32) * 4
    and_mask = bytearray(row_bytes * h)
    for y in range(h):
        for x in range(w):
            _, _, _, a = pixels[x, h - 1 - y]
            if a == 0:
                and_mask[y * row_bytes + x // 8] |= 1 << (7 - (x % 8))

    return bytes(bih) + bytes(xor) + bytes(and_mask)


def hexagon_points(cx, cy, r):
    """Return points for a regular hexagon with top and bottom vertices."""
    pts = []
    for i in range(6):
        angle = math.pi / 6 + i * math.pi / 3  # start at top-left vertex
        pts.append((cx + r * math.cos(angle), cy + r * math.sin(angle)))
    return pts


def draw_icon(bg_rgb):
    """Draw a rounded status badge containing a white hexagon."""
    img = Image.new("RGBA", (MASTER_SIZE, MASTER_SIZE), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    d.rounded_rectangle(
        (0, 0, MASTER_SIZE - 1, MASTER_SIZE - 1),
        radius=64,
        fill=bg_rgb + (255,),
    )

    pts = hexagon_points(MASTER_SIZE / 2, MASTER_SIZE / 2, 74)
    pts_int = [(round(x), round(y)) for x, y in pts]
    d.polygon(pts_int, fill=(255, 255, 255, 255))

    return img.resize((ICON_SIZE, ICON_SIZE), Image.Resampling.LANCZOS)


for color, name in [((0x2E, 0x8B, 0x57), "green"),
                    ((0xE0, 0xA0, 0x00), "yellow"),
                    ((0xC0, 0x39, 0x2B), "red")]:
    img = draw_icon(color)
    data = make_bmp_resource(img)
    path = f"{name}.bin"
    with open(path, "wb") as f:
        f.write(data)
    print(f"  {path} saved ({len(data)} bytes)")
print("done")
