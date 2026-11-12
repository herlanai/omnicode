import AppKit

// Render installer chrome at 2x; the source app icon is reused unchanged.
let args = CommandLine.arguments
 guard args.count == 3, let logo = NSImage(contentsOfFile: args[1]),
 let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1280, pixelsHigh: 840,
 bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
 colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
 let context = NSGraphicsContext(bitmapImageRep: bitmap) else { fatalError("Cannot create installer background") }
bitmap.size = NSSize(width: 640, height: 420)
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context
context.cgContext.scaleBy(x: 2, y: 2)
NSColor(calibratedWhite: 0.97, alpha: 1).setFill()
NSRect(x: 0, y: 0, width: 640, height: 420).fill()
func text(_ value: String, _ rect: NSRect, _ size: CGFloat, _ weight: NSFont.Weight, _ color: NSColor, centered: Bool = false) {
 let style = NSMutableParagraphStyle()
 style.alignment = centered ? .center : .left
 (value as NSString).draw(in: rect, withAttributes: [.font: NSFont.systemFont(ofSize: size, weight: weight), .foregroundColor: color, .paragraphStyle: style])
}
logo.draw(in: NSRect(x: 42, y: 313, width: 72, height: 72))
text("Omnicode", NSRect(x: 128, y: 342, width: 450, height: 37), 28, .semibold, .black)
text("安静、专注的原生代码编辑器", NSRect(x: 130, y: 315, width: 450, height: 24), 14, .regular, .darkGray)
NSColor(calibratedWhite: 0.87, alpha: 1).setStroke()
let divider = NSBezierPath()
divider.move(to: NSPoint(x: 48, y: 289)); divider.line(to: NSPoint(x: 592, y: 289)); divider.lineWidth = 1; divider.stroke()
text("将 Omnicode 拖到 Applications，即可安装", NSRect(x: 35, y: 238, width: 570, height: 27), 16, .medium, .darkGray, centered: true)
let arrow = NSBezierPath()
arrow.move(to: NSPoint(x: 296, y: 168)); arrow.line(to: NSPoint(x: 344, y: 168))
arrow.move(to: NSPoint(x: 334, y: 178)); arrow.line(to: NSPoint(x: 344, y: 168)); arrow.line(to: NSPoint(x: 334, y: 158))
arrow.lineWidth = 2; arrow.lineCapStyle = .round; arrow.lineJoinStyle = .round
NSColor(calibratedWhite: 0.55, alpha: 1).setStroke(); arrow.stroke()
text("安装完成后，从 Applications 打开 Omnicode", NSRect(x: 35, y: 37, width: 570, height: 23), 12, .regular, .gray, centered: true)
NSGraphicsContext.restoreGraphicsState()
guard let data = bitmap.representation(using: .png, properties: [:]) else { fatalError("PNG encoding failed") }
try data.write(to: URL(fileURLWithPath: args[2]))
