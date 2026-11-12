on run argv
    set mountPath to item 1 of argv
    tell application "Finder"
        set installerFolder to POSIX file mountPath as alias
        open installerFolder
        set installerWindow to container window of installerFolder
        set current view of installerWindow to icon view
        set toolbar visible of installerWindow to false
        set statusbar visible of installerWindow to false
        set bounds of installerWindow to {160, 160, 800, 608}
        set viewOptions to icon view options of installerWindow
        set arrangement of viewOptions to not arranged
        set icon size of viewOptions to 88
        set text size of viewOptions to 13
        set background picture of viewOptions to file ".background:installer.png" of installerFolder
        set position of item "Omnicode.app" of installerFolder to {180, 252}
        set position of item "Applications" of installerFolder to {460, 252}
        update installerFolder without registering applications
        delay 2
        close installerWindow
    end tell
end run
