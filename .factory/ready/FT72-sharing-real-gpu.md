---
source: pr #70
branch: factory/FT72-sharing-real-gpu
round: 1
---
# Sharing: capture and encode on a real GPU instead of the VM screen

## factory · 2026-11-06 19:02
Added to review at the owner's request: “PR 70 也加入 review status”.
Source: https://github.com/lyricat/inua/pull/70

The PR selects the first non-emulated GPU render node for Sunshine's adapter_name alongside capture = wlr in Sharing.prepare. When all render nodes are emulated, it leaves adapter_name empty so Sunshine chooses as before. It aims to fix black-screen capture and render-node descriptor exhaustion in VMs with a passed-through GPU.

The author reports go vet and remote-desktop package tests passing, and a working Moonlight session on an Intel iGPU passthrough guest. These claims have not been independently verified here; factory review and make test are pending.

The PR comes from a fork. Its head was copied unchanged to the repository branch above for factory review.
Imported head: 2cc1861671577309e930dbc1c13c5a113d4879df
No implementation changes, review, or merge performed during intake.

## factory · review · 2026-11-06 19:22
head: 2cc1861671577309e930dbc1c13c5a113d4879df
verdict: pass
notes:
- Codex reviewed all three changed files and relevant callers against origin/master (70207886b4317419a0d1cb07009098f549ad6ea9); no qualifying findings. The branch merges cleanly with base.
- Validation: make test passed (18 script tests, go vet ./..., go test -race ./...); git diff --check passed. Tests used a throwaway HOME; worktree remains clean.
- Installs: none. No dependency downloads.
- Review-code skill was unavailable to the coder; it used the supplied evidence-based review criteria.
- Not independently checked: UI appearance, real Sunshine/Moonlight streaming, Intel GPU passthrough, and resolution of black-screen capture or descriptor exhaustion. Owner should verify these on real hardware before shipping.
