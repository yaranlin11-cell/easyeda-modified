#!/usr/bin/env python3
"""Prepare a source-preserving KiCad reference with an explicit requirement stackup.

Never silently deletes copper caches, replaces custom pads or changes a via span.
Output is an offline candidate, not a verified EasyEDA project.
"""
import argparse
import copy
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import zipfile
from kicad_reference import parse, emit, children, one, inventory, semantic_digest


def positive(value):
    n = Decimal(str(value))
    if not n.is_finite() or n <= 0:
        raise ValueError('thickness must be finite and positive')
    return n


def apply_stackup(board, recipe):
    if recipe['provenanceKind'] != 'requirement' or not recipe['locator'].strip():
        raise ValueError('explicit requirement provenance and locator required')
    copper = [positive(x) for x in recipe['copperUm']]
    dielectric = [positive(x) for x in recipe['dielectricUm']]
    mask = [positive(x) for x in recipe['maskUm']]
    types = recipe['dielectricTypes']
    materials = recipe['dielectricMaterials']
    if len(types) != len(dielectric) or len(materials) != len(dielectric) or any(x not in ('core', 'prepreg') for x in types) or any(not isinstance(x, str) or not x.strip() for x in materials):
        raise ValueError('explicit dielectric types and materials required')
    names = [x[1] for x in one(board, 'layers')[1:] if isinstance(x, list) and x[1].endswith('.Cu"')]
    # KiCad serializes B.Cu before inner layers in some versions.
    names.sort(key=lambda x: 0 if x == '"F.Cu"' else 100 if x == '"B.Cu"' else int(x.split('In')[1].split('.')[0]))
    if len(copper) != len(names) or len(dielectric) != len(copper)-1 or len(mask) != 2:
        raise ValueError('layer count mismatch')
    total = sum(copper + dielectric + mask)
    if total != positive(recipe['finishedThicknessUm']):
        raise ValueError('stackup sum differs from required finished thickness')
    mm = lambda n: format(n / 1000, 'f')
    stackup = ['stackup', ['layer', '"F.Mask"', ['type', '"Top Solder Mask"'], ['thickness', mm(mask[0])], ['material', json.dumps(recipe['maskMaterial'])]]]
    for i, (name, thickness) in enumerate(zip(names, copper)):
        stackup.append(['layer', name, ['type', '"copper"'], ['thickness', mm(thickness)]])
        if i < len(dielectric):
            stackup.append(['layer', json.dumps('dielectric ' + str(i+1)), ['type', json.dumps(types[i])], ['thickness', mm(dielectric[i])], ['material', json.dumps(materials[i])]])
    stackup.append(['layer', '"B.Mask"', ['type', '"Bottom Solder Mask"'], ['thickness', mm(mask[1])], ['material', json.dumps(recipe['maskMaterial'])]])
    setup = one(board, 'setup')
    if children(setup, 'stackup'):
        raise ValueError('existing stackup requires explicit reconciliation; will not overwrite')
    setup.append(stackup)
    one(one(board, 'general'), 'thickness')[1] = mm(total)
    return total


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--board', type=Path, required=True)
    p.add_argument('--stackup', type=Path, required=True)
    p.add_argument('--output-dir', type=Path, required=True)
    a = p.parse_args()
    raw = a.board.read_bytes()
    recipe = json.loads(a.stackup.read_text())
    source = Path(recipe['sourcePath'])
    if hashlib.sha256(source.read_bytes()).hexdigest() != recipe['sourceSha256']:
        raise ValueError('requirement source hash mismatch')
    board = parse(raw.decode('utf-8'))
    before = inventory(board)
    original = copy.deepcopy(board)
    apply_stackup(board, recipe)
    rendered = emit(board) + '\n'
    reread = parse(rendered)
    after = inventory(reread)
    assert board == reread
    restored = copy.deepcopy(reread)
    setup = one(restored, 'setup')
    setup.remove(one(setup, 'stackup'))
    one(one(restored, 'general'), 'thickness')[1] = before['boardThicknessMm']
    assert restored == original, 'unexpected geometry or network modification'
    assert {k:v for k,v in before.items() if k != 'boardThicknessMm'} == {k:v for k,v in after.items() if k != 'boardThicknessMm'}
    a.output_dir.mkdir(parents=True, exist_ok=False)
    target = a.output_dir / 'reference.kicad_pcb'
    target.write_text(rendered)
    with zipfile.ZipFile(a.output_dir/'reference-kicad.zip','w',zipfile.ZIP_DEFLATED) as z:
        z.write(target,target.name)
    report = {'status':'offline-candidate-not-live-verified','inputSha256':hashlib.sha256(raw).hexdigest(),
              'outputSha256':hashlib.sha256(target.read_bytes()).hexdigest(),'requirement':recipe,
              'before':before,'after':after,'geometryAndNetworksPreserved':True,
              'limitations':['Thickness is applied from requirement, not recovered from native BRD.',
                             'Dielectric types and materials were applied from the supplied requirement recipe.',
                             'Host import, full object readback and manufacturing acceptance remain required.']}
    (a.output_dir/'manifest.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print(json.dumps(after,indent=2))

if __name__ == '__main__':
    main()
