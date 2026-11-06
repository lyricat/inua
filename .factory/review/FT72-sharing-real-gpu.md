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
