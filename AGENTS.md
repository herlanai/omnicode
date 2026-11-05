# Application conventions

- Keep Omnicode's native Golet UI minimal, using icon buttons with tooltips for toolbar actions.
- Main and app-owned child windows (settings, diff review, and document preview) use `TitleBarStyle: golet.TitleBarHiddenInset` and `TrafficLightPosition: &golet.Point{X: 12, Y: 11}`, following Feo, unless the user explicitly requests another style. Platform-specific dialogs and utility windows are exempt. This is an application convention, not a change to Golet's zero-value `WindowOptions` behavior.
- Custom title bars must reserve `c.TitleBar()` system-control insets and provide a draggable blank region without blocking toolbar controls.
- After each change, run appropriate checks and produce a fresh macOS DMG installer including its app icon and an Applications shortcut. Deliver the DMG, not a ZIP archive.
