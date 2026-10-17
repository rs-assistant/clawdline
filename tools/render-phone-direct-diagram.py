#!/usr/bin/env python3
"""Render the phone-to-machine direct connection diagram in both README languages."""

from html import escape
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]

COPY = {
    "en": {
        "title": "A faster path from your phone to your machine",
        "subtitle": "Terminal and Session reads use a direct WebRTC data channel when ICE can connect.",
        "phone": "Your phone",
        "browser": "Clawdline browser",
        "machine": "Your machine",
        "platform": "Mac or Linux",
        "direct": "DIRECT CONNECTION",
        "traffic": "Terminal + Session reads",
        "relay": "Clawdline Cloud",
        "fallback": "Encrypted signalling + fallback",
        "note": "If direct ICE cannot connect, the encrypted relay keeps the session available.",
        "desc": "A phone and a Mac or Linux machine exchange terminal and Session reads over a direct WebRTC data channel. Clawdline Cloud carries signalling and provides an encrypted relay fallback.",
    },
    "zh-Hant": {
        "title": "手機直連你的機器，少一道轉送",
        "subtitle": "ICE 可連通時，終端機與 Session 讀取走 WebRTC 直連資料通道。",
        "phone": "你的手機",
        "browser": "Clawdline 瀏覽器",
        "machine": "你的機器",
        "platform": "Mac 或 Linux",
        "direct": "直連資料通道",
        "traffic": "終端機＋Session 讀取",
        "relay": "Clawdline Cloud",
        "fallback": "加密協商＋備援轉送",
        "note": "ICE 無法直連時，端對端加密的 Relay 仍讓你接得上 Session。",
        "desc": "手機與 Mac 或 Linux 機器透過 WebRTC 資料通道直連，傳送終端機與 Session 讀取；Clawdline Cloud 負責連線協商，並在直連失敗時提供加密 Relay 備援。",
    },
}


def render(lang: str, labels: dict[str, str]) -> str:
    t = {key: escape(value) for key, value in labels.items()}
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="610" viewBox="0 0 1200 610" role="img" aria-labelledby="title desc" xml:lang="{lang}">
<title id="title">{t["title"]}</title>
<desc id="desc">{t["desc"]}</desc>
<defs><marker id="arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M1 1 9 5 1 9" fill="none" stroke="#9da4ed" stroke-width="2"/></marker></defs>
<rect width="1200" height="610" rx="24" fill="#16161a"/>
<g font-family="Inter, Noto Sans TC, system-ui, sans-serif">
<text x="56" y="68" fill="#e8e6e3" font-size="30" font-weight="700">{t["title"]}</text>
<text x="56" y="102" fill="#9a978f" font-size="17">{t["subtitle"]}</text>
<rect x="56" y="160" width="270" height="210" rx="16" fill="#131317" stroke="#24242b"/>
<rect x="82" y="188" width="48" height="88" rx="10" fill="#1c1c22" stroke="#9da4ed" stroke-width="2"/>
<rect x="89" y="199" width="34" height="57" rx="3" fill="#24242b"/>
<circle cx="106" cy="265" r="3" fill="#9da4ed"/>
<text x="149" y="217" fill="#e8e6e3" font-size="22" font-weight="700">{t["phone"]}</text>
<text x="149" y="247" fill="#9a978f" font-size="15">{t["browser"]}</text>
<rect x="82" y="311" width="218" height="34" rx="8" fill="#1c1c22"/>
<text x="96" y="334" fill="#9da4ed" font-size="14">●  Session</text>
<rect x="874" y="160" width="270" height="210" rx="16" fill="#131317" stroke="#24242b"/>
<rect x="900" y="188" width="47" height="44" rx="6" fill="#1c1c22" stroke="#9da4ed" stroke-width="2"/>
<path d="M910 200l8 6-8 6M923 216h12" fill="none" stroke="#9da4ed" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
<text x="969" y="217" fill="#e8e6e3" font-size="22" font-weight="700">{t["machine"]}</text>
<text x="969" y="247" fill="#9a978f" font-size="15">{t["platform"]}</text>
<rect x="900" y="311" width="218" height="34" rx="8" fill="#1c1c22"/>
<text x="914" y="334" fill="#9da4ed" font-size="14">●  Clawdline</text>
<path d="M335 265H865" stroke="#9da4ed" stroke-width="5" marker-start="url(#arrow)" marker-end="url(#arrow)"/>
<rect x="397" y="178" width="406" height="66" rx="12" fill="#1c1c22" stroke="#9da4ed"/>
<text x="600" y="204" fill="#9da4ed" text-anchor="middle" font-size="13" font-weight="700" letter-spacing="1">{t["direct"]}</text>
<text x="600" y="226" fill="#e8e6e3" text-anchor="middle" font-size="16">{t["traffic"]}</text>
<path d="M191 370V453H461M739 453H1009V370" fill="none" stroke="#9a978f" stroke-width="2" stroke-dasharray="7 7"/>
<rect x="461" y="410" width="278" height="84" rx="13" fill="#1c1c22" stroke="#24242b"/>
<text x="600" y="442" fill="#e8e6e3" text-anchor="middle" font-size="18" font-weight="700">{t["relay"]}</text>
<text x="600" y="468" fill="#9a978f" text-anchor="middle" font-size="14">{t["fallback"]}</text>
<text x="600" y="557" fill="#9a978f" text-anchor="middle" font-size="16">{t["note"]}</text>
</g></svg>
'''


for locale, copy in COPY.items():
    (ROOT / "docs/assets" / f"phone-direct-{locale}.svg").write_text(render(locale, copy))
