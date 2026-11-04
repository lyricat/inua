# Copying assistant messages

Each user message and assistant answer has a Copy button using the shared
`outlined small` control styles. Copy reads the label at activation time
and writes its full plain text to the standard GDK clipboard. It copies
one message, without role names, tool lines or approval cards. No external
clipboard command is needed.

The waiting ellipsis has a disabled Copy button. As text arrives, Copy
becomes available; an empty update disables it again. Displayed error,
stopped and no-answer text uses the same action. If the display or clipboard
is unavailable, an error appears under that message. The action does not
claim that a paste succeeded.

All Copy buttons share one native signal callback. The conversation owns
their widgets, and the button lookup is cleared when the conversation is
reset or cleared and on shutdown. Tool lines and approval cards are inserted
before the whole answer row. Adding rows and clicking Copy do not move
focus from the question entry; keyboard navigation can focus Copy normally.

## Manual checks

FT60 was not checked by eye. The following checks remain to be performed in
an isolated desktop session with a test assistant profile and a plain-text
editor for pasting:

1. Open an empty conversation. Suggestions have no message Copy actions.
   Submit a question, copy it, and paste into the editor. Copy the answer
   separately and confirm that neither paste includes other messages,
   tool lines or role names.
2. Submit a multiline question through `inua-assistant --ask` with actual
   line breaks and Unicode text, such as Chinese, Japanese and emoji. Ask
   for a multiline answer. Copy each role's message and check the pasted
   text, line breaks, spacing and characters against what is displayed.
   Select part of each message with the pointer and check selection copying
   through its context menu as well.
3. Request a long answer. Before its first text arrives, confirm Copy is
   disabled for the waiting ellipsis. Copy during streaming, paste, then
   copy again after more text arrives. Each paste must contain the current
   text at the time of that click. Check the completed answer too.
4. Stop an answer before text arrives and again after partial text arrives.
   Copy and paste the displayed result. With a test provider returning an
   error or an empty answer, check that the displayed error or no-answer
   text is copyable.
5. Ask for a tool action that requires approval. Check that the tool lines
   and approval card remain before the answer and that Allow and Don't
   still work. Copy must affect only the message beside it.
6. From the question entry, reach both roles' Copy buttons using Tab and
   Shift+Tab. Activate each with Space and Enter, then paste into the editor.
   Confirm a visible focus indicator. While typing a new question, click
   Copy with the pointer and confirm typing can continue in the entry.
   Streaming text must not move focus to a new Copy button.
7. Start a new conversation with + after the answer finishes. Check that
   old messages and their actions disappear, suggestions return without
   Copy actions, and new messages copy their own text. Repeat several times,
   then close and reopen the hidden window and finally quit the test app.
   Check for GTK warnings or crashes.
8. In a disposable clipboard failure harness, exercise an unavailable or
   closed display and a missing clipboard. Copy must not crash or report
   success; its error belongs under that message. Retry with a working
   clipboard and confirm the error clears and the text pastes correctly.

These are UI interaction checks; no new automated test was added for them.
