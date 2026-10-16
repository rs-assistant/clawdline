# The website's feature pictures

clawdline.com shows three real console scenes on a phone. They are the console as it is built,
photographed against a stand-in daemon that serves one small invented story, so a reader who has
never seen Clawdline can follow them:

1. An Agent rebuilding a shop's checkout asks whether to ship it now, with two suggested replies.
2. The person queues the next fix for that Session — the Pay button is cut off on iPhone — with a
   screenshot attached.
3. The Board item for the checkout shows the stage it is in and the Session doing it.

The story lives in `STORIES` in `web/console/src/session/site-shots.ts`, written separately in
English and Taiwan Traditional Chinese, so neither language's pictures read as a translation. It
used to be the e2e fixtures' own sample content ("Bravo is still working", `/tmp/fixture`, a
"Manager Session"), translated string by string; that content exists to test edge cases, and in a
picture it read as noise. The ordinary e2e fixtures no longer know the website exists.

## Recapturing

```sh
tools/site-shots.sh <website checkout>/marketing/marketing-claude/public/features
```

The script builds the console through `tools/heavy.sh`, then runs
`web/console/src/session/site-shots.e2e.ts` once per language. Each run writes
`<scene>-en.png` or `<scene>-zh-Hant.png`: a 390 × 844 phone at 2×, so 780 × 1688 pixels. Without
`SITE_SHOTS` those tests skip.

## The callouts

Each picture marks what to notice with a hairline outline at the real element's box and a small
number at its corner; the rest of the screen is lightly shaded. The picture carries no legend:
the website's gallery (`marketing/marketing-claude/src/components/ScreenshotGallery.astro` in the
website repository) names each number under the picture and in its lightbox, in the words of
`SITE_CALLOUTS`. A change to one is a change to both, in both languages.

## Before publishing

Look at all six pictures. A fold caught mid-animation, a callout around nothing, a name cut short
in the header, or a label in the wrong language is a capture to fix here, not a picture to
publish.
