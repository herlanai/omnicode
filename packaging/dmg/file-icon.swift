import AppKit

// A disk image's Finder icon is separate from its mounted volume icon.
let args = CommandLine.arguments
guard args.count == 3, let icon = NSImage(contentsOfFile: args[1]) else {
    fatalError("Usage: file-icon.swift icon.icns installer.dmg")
}
guard NSWorkspace.shared.setIcon(icon, forFile: args[2], options: []) else {
    fatalError("Cannot set installer file icon: \(args[2])")
}
