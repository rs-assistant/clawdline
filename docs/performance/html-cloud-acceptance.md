# Authenticated Cloud HTML acceptance

This record is for maintainers checking the static HTML document path after deployment. It complements the [local browser measurements](html-artifact-browser.json); it does not replace them or establish a capacity limit.

## Fixed subjects and isolation

On 2026-10-20, an independent verifier used the production hosted console and a dedicated Chrome profile. A local Linux container ran daemon commit `3807571d89c08de6529444414eed52945605efe5`, with executable SHA-256 `cd74b09d87faf62c1dec9a07644ef6d867d860a69b64a9420928f09bae579365`. The hosted console stamp was `9cc4e60e4e0055c0a97805cbd9ee0e65ca59ca7e`, containing the HTML implementation. Its served main bundle contained CloudGate.

The container had its own filesystem, process namespace, state, two synthetic projects and two synthetic tmux Sessions. An empty daemon state on the host alone would not isolate host Session discovery. No host port was published. Cloud commands were disabled. The person completed login and confirmed both fingerprints before the dedicated machine and viewer were paired only to each other. Existing machine pairings were not changed.

## Live checks

| Check | Control | Observation |
| --- | --- | --- |
| Exact Session destination | Same machine, scope and filename; change only Session | A and B each rendered their own distinct synthetic canary, 57 sanitized UTF-8 bytes each. Switching removed the previous iframe while loading. |
| Markdown compatibility | Same synthetic B content in Markdown | Correct B content, 46 bytes. |
| Unpaired machine UI | Dedicated machine paired; existing machines unpaired to this viewer | Existing machines showed pairing gates. No private Session was opened. This is not a cross-account test. |
| Offline rejection | Stop only the verified test daemon PID after a successful read | Fresh read showed existing `machine_offline`; old iframe was removed and sharing disabled. |
| Recovery | Restart the same binary with the same state | Health and console HTTP 200 verified; A and B again returned their correct 57-byte HTML. |
| Local pairing revocation | Revoke only the dedicated viewer on the test machine, then change both canaries | Fresh reads returned neither changed marker. Machine logs recorded dropped envelopes from the locally revoked sender. The document page eventually displayed existing `cloud_read_timeout`, with sharing and link copy disabled. |

The last check exercises machine-local pin revocation, not account-wide device revocation or control-plane propagation. Initial observation at 73 seconds still showed loading; a later observation showed the timeout. That finite observation alone did not establish an indefinitely pending read. A fresh foreground repeat began at 01:10:54.711 UTC and showed `cloud_read_timeout` at 01:11:58.384 UTC: 63,673 ms between observations, an upper observation bound rather than exact settlement latency. The existing read timer is 60 seconds, with a conditional status probe of up to 10 seconds and a possible preceding connection wait of 10 seconds. The document path does not use the separate coherent-Session wait. The cause of the earlier 73-second intermediate observation was not established. No new deadline, error code or protocol was introduced.

## Measurements and interpretation

The original decoder refused HTML; the current decoder renders it. These are different outcomes, not a before/after speedup. The existing local fixed-file samples used one document fetch and 7,565 bytes each. Chromium mount times were 14.5 ms at 390 CSS pixels and 4.4 ms at 1280; WebKit mount times were 11 and 7 ms. Ready time includes automation waiting. Markdown samples are often below timer resolution, and heap samples are coarse or unavailable. Do not infer production latency or memory savings from these component samples.

The live checks above add authenticated routing and failure-path evidence. They did not measure production request totals, NIC bytes, throughput, saturation or CPU. Those belong to the separate Cloud cost baseline. This acceptance stage changes no production parser or runtime behavior.

## Boundaries

Prior independent source review found no blocking exploit path in the static HTML changes. Prior Chromium/WebKit component checks attempted scripts, parent DOM access, navigation, popups and resource loads; the collector received zero requests. Prior production Chrome layout checks used 390 and 1280 CSS pixels. These are separate evidence sets, not attacks rerun through this authenticated fixture. Actual phone Safari, cross-account access and account-control-plane revocation were not tested here.

Private machine/viewer identifiers, credentials and raw user-interface transcripts are deliberately excluded from this public record. The root retains the independent verifier's private report and runtime/refusal evidence. After testing, the root confirmed the dedicated viewer pin was revoked, disabled Cloud on the test daemon, stopped only the verified test PID and private tmux server, and removed the stopped container and its task-owned fixture/state, including test keys. The dedicated Chrome profile's Cloud site data was cleared; a fresh reload showed the GitHub sign-in gate. No account-wide device deletion or existing pairing mutation was performed. An offline account roster entry may remain; it has no running test daemon or retained local machine key.
