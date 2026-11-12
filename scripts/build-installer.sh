#!/bin/bash
set -eu
app=$1
stage=$2
arch=$3
pkg=$4
mkdir -p "$stage/pkg-resources" "$stage/pkg-root/Applications"
cp packaging/installer/*.html "$stage/pkg-resources/"
cp assets/omnicode-icon.png "$stage/pkg-resources/logo.png"
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")
ditto "$app" "$stage/pkg-root/Applications/Omnicode.app"
cat > "$stage/components.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><array><dict>
<key>RootRelativeBundlePath</key><string>Applications/Omnicode.app</string>
<key>BundleIsRelocatable</key><false/>
<key>BundleIsVersionChecked</key><true/>
<key>BundleHasStrictIdentifier</key><true/>
<key>BundleOverwriteAction</key><string>upgrade</string>
</dict></array></plist>
PLIST
pkgbuild --root "$stage/pkg-root" --component-plist "$stage/components.plist" --identifier dev.omnicode.editor --version "$version" --install-location / "$stage/payload.pkg"
cat > "$stage/distribution.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
<title>Omnicode</title>
<welcome file="welcome.html" mime-type="text/html"/>
<conclusion file="conclusion.html" mime-type="text/html"/>
<options customize="never" require-scripts="false" hostArchitectures="$arch" rootVolumeOnly="true"/>
<domains enable_anywhere="false" enable_currentUserHome="false" enable_localSystem="true"/>
<allowed-os-versions><os-version min="13.5"/></allowed-os-versions>
<choices-outline><line choice="default"/></choices-outline>
<choice id="default" visible="false"><pkg-ref id="dev.omnicode.editor"/></choice>
<pkg-ref id="dev.omnicode.editor" version="$version" onConclusion="none">payload.pkg</pkg-ref>
</installer-gui-script>
XML
productbuild --distribution "$stage/distribution.xml" --resources "$stage/pkg-resources" --package-path "$stage" "$pkg"
pkgutil --expand "$pkg" "$stage/expanded-pkg"
test -f "$stage/expanded-pkg/Distribution"
# Keep HTML-referenced images even on productbuild versions that omit them.
cp "$stage/pkg-resources/logo.png" "$stage/expanded-pkg/Resources/logo.png"
pkgutil --flatten "$stage/expanded-pkg" "$stage/with-resources.pkg"
mv -f "$stage/with-resources.pkg" "$pkg"
xar -tf "$pkg" | grep -q '^Resources/logo.png$'
lsbom -s "$stage/expanded-pkg/payload.pkg/Bom" | grep -q '^./Applications/Omnicode.app/Contents/MacOS/omnicode$'
python3 - "$stage/expanded-pkg/payload.pkg/PackageInfo" <<'PYCODE'
import sys, xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
assert root.attrib['relocatable'] == 'false'
assert root.attrib['install-location'] == '/'
assert root.find('bundle').attrib['path'] == './Applications/Omnicode.app'
assert not root.findall('relocate/bundle')
PYCODE
grep -q "hostArchitectures=\"$arch\"" "$stage/expanded-pkg/Distribution"

# Validate the actual install payload, not only the app before packaging.
pkgutil --expand-full "$pkg" "$stage/verify-product"
installed="$stage/verify-product/payload.pkg/Payload/Applications/Omnicode.app"
codesign --verify --deep --strict "$installed"
lipo -verify_arch "$arch" "$installed/Contents/MacOS/omnicode"
