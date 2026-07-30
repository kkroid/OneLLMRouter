"""Generate deterministic OneLLMRouter Qt tray icons."""
import math
from pathlib import Path

from PIL import Image, ImageDraw

MASTER_SIZE = 256
ASSET_DIR = Path(__file__).resolve().parent


def hexagon_points(cx, cy, radius):
    return [
        (cx + radius * math.cos(math.pi / 6 + index * math.pi / 3),
         cy + radius * math.sin(math.pi / 6 + index * math.pi / 3))
        for index in range(6)
    ]


def draw_master_icon(background):
    image = Image.new("RGBA", (MASTER_SIZE, MASTER_SIZE), (0, 0, 0, 0))
    drawing = ImageDraw.Draw(image)
    drawing.rounded_rectangle(
        (0, 0, MASTER_SIZE - 1, MASTER_SIZE - 1),
        radius=64,
        fill=background + (255,),
    )
    drawing.polygon(
        [(round(x), round(y))
         for x, y in hexagon_points(MASTER_SIZE / 2, MASTER_SIZE / 2, 74)],
        fill=(255, 255, 255, 255),
    )
    return image


for color, name in [
    ((0x2E, 0x8B, 0x57), "green"),
    ((0xE0, 0xA0, 0x00), "yellow"),
    ((0xC0, 0x39, 0x2B), "red"),
]:
    draw_master_icon(color).save(
        ASSET_DIR / f"{name}.ico",
        format="ICO",
        sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (256, 256)],
    )
