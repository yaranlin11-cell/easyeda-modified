#!/usr/bin/env python3
"""Local Allegro conversion with provenance; never certifies import fidelity."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import zipfile
from kicad_reference import parse, inventory


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', required=True, help='Official KiCad 10+ kicad-cli executable')
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output-dir', required=True, type=Path, help='Must not already exist')
    args = parser.parse_args()
    source = args.source.resolve()
    with source.open('rb') as stream:
        header = stream.read(4)
    if len(header) != 4 or header[2:4] not in (b'\x13\0', b'\x14\0', b'\x15\0', b'\x16\0'):
        parser.error('source does not have a supported Allegro binary family header')
    version = subprocess.check_output([args.cli, 'version'], text=True).strip()
    if int(version.split('.')[0]) < 10:
        parser.error('KiCad 10+ is required; version alone does not prove source compatibility')
    args.output_dir.mkdir(parents=True, exist_ok=False)
    out = args.output_dir.resolve()
    board = out / 'reference.kicad_pcb'
    record = {'source': str(source), 'sourceSha256': sha(source), 'converterVersion': version, 'converterExecutableSha256': sha(Path(args.cli).resolve()),
              'status': 'conversion-pending', 'limitations': [
                  'No independent source/target equivalence proof.',
                  'Converter version and an empty warnings list do not establish layer-span fidelity; inspect output inventory and source implementation.',
                  'Stackup thickness, materials, rules and special pads require independent verification.',
                  'Do not manufacture or claim chain continuity from this intermediate file.'
              ]}
    command = [args.cli, 'pcb', 'import', '--format', 'auto', '--report-format', 'json',
               '--report-file', str(out / 'import-report.json'), '--output', str(board), str(source)]
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=180)
    except (OSError, subprocess.TimeoutExpired) as error:
        record.update(status='conversion-failed', error=str(error), sourceUnchanged=sha(source) == record['sourceSha256'])
        (out / 'conversion-manifest.json').write_text(json.dumps(record, ensure_ascii=False, indent=2))
        print(json.dumps(record, ensure_ascii=False, indent=2))
        return 1
    (out / 'conversion.log').write_text(result.stdout + result.stderr)
    record['exitCode'] = result.returncode
    record['sourceUnchanged'] = sha(source) == record['sourceSha256']
    valid = result.returncode == 0 and board.is_file() and board.stat().st_size > 0 and record['sourceUnchanged']
    if valid:
        try:
            record['inventory'] = inventory(parse(board.read_text(encoding='utf-8')))
        except (ValueError, UnicodeError) as error:
            valid = False
            record['validationError'] = str(error)
    if valid:
        if version == '10.0.7':
            record['limitations'].append('Known KiCad 10.0.7 limitation: Allegro vias are forced to outer layers.')
        record.update(status='converted-review-required', outputSha256=sha(board))
        with zipfile.ZipFile(out / 'reference-kicad.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
            archive.write(board, board.name)
    else:
        record['status'] = 'conversion-failed'
    (out / 'conversion-manifest.json').write_text(json.dumps(record, ensure_ascii=False, indent=2))
    print(json.dumps(record, ensure_ascii=False, indent=2))
    return 0 if valid else 1


if __name__ == '__main__':
    sys.exit(main())
