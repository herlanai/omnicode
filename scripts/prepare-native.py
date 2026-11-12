"""Fetch the target's pinned native libraries without executing target code."""
import hashlib
import json
import pathlib
import shutil
import subprocess
import sys

arch, destination, module = sys.argv[1:]
dst = pathlib.Path(destination)
cache = pathlib.Path('bin/runtime-cache/native')
cache.mkdir(parents=True, exist_ok=True)

def fetch(url, digest, target):
    cached = cache / digest
    if not cached.exists() or hashlib.sha256(cached.read_bytes()).hexdigest() != digest:
        part = cached.with_suffix('.part')
        subprocess.run(['curl', '-fsSL', '--retry', '2', url, '-o', str(part)], check=True)
        if hashlib.sha256(part.read_bytes()).hexdigest() != digest:
            raise RuntimeError('Native runtime checksum mismatch: ' + url)
        part.replace(cached)
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(cached, target)

for plugin in ('terminal', 'sqlite'):
    manifest = json.loads((pathlib.Path(module) / 'backend/plugins' / plugin / 'golet-plugin.json').read_text())
    for library in manifest['libraries']:
        entry = library['files']['darwin-' + arch]
        fetch(entry['url'], entry['sha256'], dst / entry['name'])
ocr = dst / 'open-code-review'
ocr.mkdir(exist_ok=True)
for name in ('LICENSE',):
    shutil.copyfile(pathlib.Path('resources/open-code-review') / name, ocr / name)
if arch == 'arm64':
    subprocess.run(['shasum', '-a', '256', '-c', 'SHA256SUMS'], cwd='resources/open-code-review', check=True)
    shutil.copyfile('resources/open-code-review/opencodereview', ocr / 'opencodereview')
else:
    fetch('https://github.com/alibaba/open-code-review/releases/download/v1.13.3/opencodereview-darwin-amd64',
          '093d0a402aff9715c31c02903268fb73880f8a9350cc2a5813a939ee186e5545', ocr / 'opencodereview')
(ocr / 'opencodereview').chmod(0o755)

(ocr / 'PROVENANCE.md').write_text(
    '# Open CodeReview\n\nVersion: v1.13.3. License: Apache-2.0.\n'
    'Source: https://github.com/alibaba/open-code-review/tree/v1.13.3\n'
    'Target: darwin-' + arch + '. Input SHA-256 is verified before local signing.\n'
    'Omnicode delegates model analysis to the configured AI engine.\n')
