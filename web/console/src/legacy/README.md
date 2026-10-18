# Copied stylesheets, owned code

Every `.css` file beside this one is a byte-for-byte copy of the Swift app's
console stylesheet, taken from `~/code/clawdline/Resources/web/app/css`.

They are copied rather than rewritten because the instruction was to replicate
the existing screen strictly, and a stylesheet re-typed from a screenshot is an
approximation that will diverge on the first detail nobody looked at. Reusing
the original makes the replication exact by construction: the React components
emit the same DOM and the same class names, so the same rules apply to them.

`MANIFEST.json` records the SHA-256 prefix of each stylesheet at the moment it
was copied, and `tools/check-legacy-css.sh` compares them. That guard exists
because a copy that cannot notice it changed is the defect this project keeps
finding in other places — two things that were once the same, drifting with
nothing to say so. When it goes red, the change to the stylesheet is the thing
to look at: a deliberate one updates its pin in the same commit and says why.

## The JavaScript is this console's own code

`js/` was copied from the same app, byte for byte, when this console was built.
Since that app retired on 2026-09-19 there is no original left to compare it
with, and the files have been changed in place as the console needed. They are
maintained here like any other source: edit them, test them, and delete what
nothing imports. They are not pinned, and neither is
`public/strings/zh-Hant.json`.

Both bundles tree-shake these modules, so a function nothing reaches costs no
bytes in the shipped console — but it still costs a reader. When code here is
removed as unreachable, rebuilding the local and hosted bundles and finding the
emitted JavaScript unchanged is the proof that it was.
