# Secret Handoff

Use **Secret Handoff** when a Session needs a password, credential, or API key for a tool. The Session asks in a Note that also names the exact command that will receive the value. You provide the value in that Note's dedicated field; it is never sent as a chat message.

## Hand off a value

1. Open the Session that asked for the value. Check its name, the Note's title and reason, and the credential name immediately above the input. The requesting agent sets that name.
2. Read the command under **This value will be given to:**. Clawdline gives the value only to that exact command, with those exact arguments. Continue only if it is the tool you expect, such as `/usr/bin/gh auth login --with-token` for a GitHub token. A shell (`sh -c`, `bash -c`), `curl`, `wget`, or an address you do not recognize is a reason to stop and ask the Session why. If the Note says it does not name a command, it cannot be handed off; ask the Session for a new request.
3. Paste the value into the field named for the requested credential. If the Agent did not name it, the label reads **Secret value**. The field always accepts one-line and multi-line values and shows the text while you enter it; check your surroundings before pasting.
4. Select **Hand off securely**. The button shows progress, then a green check confirms the machine received the value before the panel folds away. Reopen the Note to see the receipt or replace an unclaimed value. A page reload also keeps the receipt visible. Clawdline does not display the value again.
5. The named Session can use the value once, within 15 minutes, and only with the command you saw. After it uses the value, the Note moves to handled. If the Session tries a different command, it receives nothing, the value is wiped, and the Note also moves to handled without the Session reporting success. If the machine restarts or the time runs out first, ask the Session for a fresh Secret Handoff Note.

Do not paste the value into a suggested reply, a normal message, a document, or a command argument. A normal message becomes part of the conversation record. A Secret Handoff value stays in the target machine's memory until use or expiry; Clawdline Cloud carries the submission encrypted. The receiving tool may still store or print what it receives, so use a tool you trust and follow its own credential guidance.

If the page says the handoff did not complete, check whether the Session received it before submitting the value again. The request may have reached the machine even if the confirmation did not return.

For the technical boundary and the agent command, see [the design note](../secret-handoff.md).
