# Files: select the default folder name and create a folder from selected items

## factory · 2026-11-15 20:31
Registered the owner's UX request for inua-file in `new/`; implementation has not started.

Requested behavior:
- When the create-folder dialog opens, focus the name field and select the entire default name so typing replaces it immediately.
- When multiple files and/or folders are selected, include a New Folder action in the selection context menu.
- Invoking that action opens the create-folder dialog and explicitly tells the user that the selected items will be moved into the new folder after it is created.
- Show the selected files/folders in that dialog so the user can see what will move.
- On confirmation, create the new folder in the current directory and move the selected items into it.

Acceptance checks:
- Ordinary folder creation still works; its default name is selected on opening.
- Multi-selection works for files, folders, and mixed selections.
- Cancelling the dialog creates nothing and moves nothing.
- Invalid names or folder-creation failure move nothing; move failures are reported accurately without deleting, overwriting, or hiding unmoved items.
- Keep the existing visual design and include regression coverage for default-name selection, multi-selection creation/moves, cancellation, and errors.
