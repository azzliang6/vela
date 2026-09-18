# -*- coding: utf-8 -*-
"""生成 Vela 应用图标：墨色圆角方底 + 朱砂主帆 + 暖纸副帆 + 纸色水线。"""
from PIL import Image, ImageDraw

INK = (29, 26, 21, 255)        # 墨
VERMILION = (217, 72, 15, 255) # 朱砂
PAPER = (244, 240, 230, 255)   # 暖纸

S = 1024  # 母版尺寸，缩放输出

img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(img)

# 圆角方形底
d.rounded_rectangle([32, 32, S - 32, S - 32], radius=220, fill=INK)

# 主帆（朱砂大三角）
d.polygon([(512, 168), (512, 620), (238, 620)], fill=VERMILION)
# 副帆（暖纸小三角）
d.polygon([(560, 260), (560, 620), (768, 620)], fill=PAPER)
# 船体水线（纸色圆头粗线）
d.line([(200, 700), (824, 700)], fill=PAPER, width=34)

def save(size, name):
    img.resize((size, size), Image.LANCZOS).save(f"E:/vela-mail/src-tauri/icons/{name}")

save(512, "icon.png")
save(32, "32x32.png")
save(128, "128x128.png")
save(256, "128x128@2x.png")

# Windows ICO（多尺寸内嵌）
img.resize((256, 256), Image.LANCZOS).save(
    "E:/vela-mail/src-tauri/icons/icon.ico",
    sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)],
)

# macOS ICNS（Pillow 9.1+ 支持 ICNS 写入；失败则留给 tauri icon 生成）
try:
    img.resize((1024, 1024), Image.LANCZOS).save("E:/vela-mail/src-tauri/icons/icon.icns")
    print("icns ok")
except Exception as e:
    print("icns skipped:", e)

print("icons done")
