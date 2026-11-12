# Omnicode application icon

Created with the built-in image_gen tool. Source asset: `omnicode-icon.png`.
The brackets suggest an open O, with an insertion caret at its center. Warm white and charcoal complement the editor's light UI. The transparent canvas is preserved when generating the macOS icon sizes.

## Generation prompt

Use case: logo-brand. Design a finished macOS app icon for Omnicode, a minimal native code editor. Single square 1024x1024 asset. Transparent outside a warm off-white rounded-square macOS tile with generous approximately 10% outer margins, subtle soft edge and tiny restrained shadow. In the center, one bold charcoal geometric symbol: opposing code angle brackets joined conceptually into a distinctive open hexagonal O, with a short vertical insertion-caret stroke in the center. Balanced symmetric, strong readable silhouette at 32px. Flat precision vector-like graphic, premium restrained Swiss design, almost monochrome, no gloss, no gradients on the symbol, no text, no letters or wordmark, no extra objects, no mockup, no multiple variants. Tile occupies about 80% of canvas; central symbol occupies 48%. Actual transparent surrounding pixels.

## Packaging

`make bundle` generates the full macOS `.icns` from the source PNG with `sips` and `iconutil`, embeds it in `Contents/Resources`, and makes a drag-to-install DMG containing Omnicode.app and an Applications shortcut. The installer window reuses the logo in its background and the mounted volume icon. The Go executable embeds the same PNG for its Dock icon when launched directly.

The DMG file itself receives the same custom Finder icon during packaging. This uses macOS resource-fork and Finder metadata; copies or downloads that discard extended attributes may show the system disk-image icon, while the icon inside the mounted image remains intact.
