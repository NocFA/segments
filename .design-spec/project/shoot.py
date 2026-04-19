import time, pathlib
from playwright.sync_api import sync_playwright

BASE = "http://127.0.0.1:8777"
OUT = pathlib.Path(__file__).resolve().parents[2] / "docs" / "screenshots"
OUT.mkdir(parents=True, exist_ok=True)

# All three apps default to 'list'. Press 'g' then 'l'/'k'/'g' to switch.
# (slug, target_view_key_after_g_prefix, output_filename)
SHOTS = [
    ("Segments%20-%20A%20Obsidian.html", None, "obsidian-list.png"),
    ("Segments%20-%20B%20Console.html",  "g",  "console-graph.png"),
    ("Segments%20-%20C%20Dossier.html",  "k",  "dossier-kanban.png"),
]

VIEWPORT = {"width": 1440, "height": 900}
DPR = 2

with sync_playwright() as p:
    browser = p.chromium.launch()
    ctx = browser.new_context(viewport=VIEWPORT, device_scale_factor=DPR)
    for slug, key, out in SHOTS:
        page = ctx.new_page()
        page.goto(f"{BASE}/{slug}", wait_until="networkidle")
        page.wait_for_selector("#root > *", timeout=15000)
        time.sleep(1.2)  # let babel compile + first render settle
        if key is not None:
            page.keyboard.press("g")
            time.sleep(0.05)
            page.keyboard.press(key)
            time.sleep(1.8)  # let transition + any graph layout settle
        else:
            time.sleep(0.6)
        dest = OUT / out
        page.screenshot(path=str(dest), full_page=False)
        print(f"wrote {dest}")
        page.close()
    browser.close()
