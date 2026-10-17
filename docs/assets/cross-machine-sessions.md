# Cross-machine collaboration diagram

The diagram uses the marketing site's neutral surfaces, warm text and violet peer-traffic color. A pixel mark identifies each Session; assistant logos identify its provider. Apple and Linux marks identify the two machines, not the assistant.

- Apple and Linux paths: Simple Icons, revision `4d29a35c954699b389a834aacc97dd6e7e74143c`, https://github.com/simple-icons/simple-icons, CC0 (vendor/simple-icons-CC0.txt).
- Claude and OpenAI paths: existing marketing `AssistantLogo.astro`, from Lobe Icons, https://github.com/lobehub/lobe-icons, MIT (vendor/lobe-icons-MIT.txt). Codex uses the same OpenAI knot as the product's Session list.
- Logos identify compatible tools and platforms; they do not imply endorsement.

Run `python3 tools/render-cross-machine-diagram.py` to regenerate both SVG locales. The website uses a responsive HTML diagram with the same palette and existing assistant/Session components.
