# Secret Handoff / 機密交付

Use **Secret Handoff** when a Session needs a password, credential, or API key for a tool. The Session asks in a Note. You provide the value in that Note's dedicated field; it is never sent as a chat message.

## Hand off a value

1. Open the Session that asked for the value. Check the Note's title, reason, and Session name before entering anything.
2. In the **Secret Handoff** Note, paste the value into **Secret value**. The normal field hides one-line values. Choose **Multi-line credential** before pasting a certificate or other multi-line value; that field shows the text while you enter it.
3. Select **Hand off securely**. The confirmation says the machine received the value. It does not display the value again.
4. The named Session can use the value once, within 15 minutes. After it uses the value, the Note moves to handled. If the machine restarts or the time runs out first, ask the Session for a fresh Secret Handoff Note.

Do not paste the value into a suggested reply, a normal message, a document, or a command argument. A normal message becomes part of the conversation record. A Secret Handoff value stays in the target machine's memory until use or expiry; Clawdline Cloud carries the submission encrypted. The receiving tool may still store or print what it receives, so use a tool you trust and follow its own credential guidance.

If the page says the handoff did not complete, check whether the Session received it before submitting the value again. The request may have reached the machine even if the confirmation did not return.

For the technical boundary and the agent command, see [the design note](../secret-handoff.md).
