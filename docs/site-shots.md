# The website's feature pictures

clawdline.com shows three real console scenes: an attention Note with suggested replies, a Session
to-do with an attachment, and a Board item's review and assignment. They come from this
repository's own e2e fixtures, so they show the console as it is built, with anonymous sample
data, and they change when the console does.

## Recapturing

```sh
tools/site-shots.sh <website checkout>/marketing/marketing-claude/public/features
```

The script builds the console through `tools/heavy.sh`, then runs the capture tests named
`site shot: …` in `web/console/src/session/human-interventions.e2e.ts` and
`list-gestures.e2e.ts` once per language. Each writes `<scene>-en.png` and `<scene>-zh-Hant.png`:
a 390 × 844 phone at 2×, so 780 × 1688 pixels.

Without `SITE_SHOTS` those tests skip, and the fixtures behave exactly as the ordinary suite
expects. With it, `web/console/src/session/site-shots.ts` switches the console's language, puts
the sample content of a Traditional Chinese picture in Traditional Chinese as well, and draws the
callouts.

## The callouts

Each picture carries its own numbered callouts: the real elements' boxes are outlined where the
page drew them, the rest of the screen is dimmed, and a legend at the bottom names each number.
The pictures therefore make sense in the website's lightbox, where no page caption is beside them.

The words live in `SITE_CALLOUTS` in `site-shots.ts`. The website's gallery
(`marketing/marketing-claude/src/components/ScreenshotGallery.astro` in the website repository)
repeats them word for word as each picture's caption, so a change to one is a change to both, in
both languages.

## Before publishing

Look at all six pictures. A fold caught mid-animation, a callout around nothing, or a label in the
wrong language is a capture to fix here, not a picture to publish.
