#!/usr/bin/env python3
"""Compare source reference inventory with typed EasyEDA readback.

This checks counts, per-pin networks and positions. It explicitly does not certify
custom-pad contours, area copper, via rules, stackup, DRC or manufacturing fitness.
"""
import argparse
import collections
import json
import math
import hashlib
from native_empty_pads import empty_pad_proofs
from pathlib import Path


def compare(source, live, tolerance_mm=0.003, empty_proofs=None):
    empty_proofs=empty_proofs or {}
    if not math.isfinite(tolerance_mm) or tolerance_mm <= 0:
        raise ValueError('positive finite position tolerance required')
    expected={};observed={};errors=[];anonymous_expected=[];anonymous_observed=[]
    if live.get('componentsAvailability') != 'available':
        errors.append({'type':'components-unavailable'})
    for category in ('routing','vias'):
        if live.get('copper',{}).get('availability',{}).get(category) != 'available':
            errors.append({'type':'copper-unavailable','category':category})
    for f in source['components']:
        for pad in f['pads']:
            key=(f['ref'],pad['number'])
            if not key[0] and not key[1]:
                anonymous_expected.append(pad);continue
            if key in expected: errors.append({'type':'duplicate-source-pad','key':key})
            expected[key]=pad
    for f in live['components']:
        if f.get('availability',{}).get('pads') != 'available' and not (f.get('primitiveId') in empty_proofs and f.get('pads') in (None,[])):
            errors.append({'type':'pads-unavailable','component':f.get('designator','')})
        for pad in f.get('pads') or []:
            key=(f.get('designator',''),pad['padNumber'])
            if not key[0] and not key[1]:
                anonymous_observed.append(pad);continue
            if key in observed: errors.append({'type':'duplicate-observed-pad','key':key})
            observed[key]=pad
    for key in sorted(expected.keys() | observed.keys()):
        a=expected.get(key); b=observed.get(key)
        if a is None or b is None:
            errors.append({'type':'missing-or-extra-pad','key':key});continue
        if a['net'] != b['net']:
            errors.append({'type':'pad-net-mismatch','key':key,'expected':a['net'],'observed':b['net']})
        delta=math.hypot(a['xMm']-b['x']*0.0254,a['yMm']+b['y']*0.0254)
        if delta > tolerance_mm:
            errors.append({'type':'pad-position-mismatch','key':key,'deltaMm':delta})
    for a in anonymous_expected:
        candidates=[b for b in anonymous_observed if a['net']==b['net'] and math.hypot(a['xMm']-b['x']*0.0254,a['yMm']+b['y']*0.0254)<=tolerance_mm]
        if len(candidates)!=1:
            errors.append({'type':'anonymous-pad-match-ambiguous-or-missing'})
        else:
            anonymous_observed.remove(candidates[0])
    if anonymous_observed:errors.append({'type':'extra-anonymous-pads','count':len(anonymous_observed)})
    copper=live.get('copper',{})
    expected_counts={'components':len(source['components']),'pads':source['padCount'],
                     'vias':source['viaCount'],'tracks':source['trackPrimitives']-source['viaCount']}
    actual_counts={'components':len(live['components']),'pads':sum(len(f.get('pads') or []) for f in live['components']),
                   'vias':len(copper.get('vias') or []),'tracks':len(copper.get('lines') or [])+len(copper.get('arcs') or [])}
    for key,value in expected_counts.items():
        if actual_counts[key] != value: errors.append({'type':'count-mismatch','category':key,'expected':value,'observed':actual_counts[key]})
    return {'status':'partial-check-passed' if not errors else 'failed','positionToleranceMm':tolerance_mm,
            'expectedCounts':expected_counts,'observedCounts':actual_counts,'errors':errors,
            'unverified':['pad contour equivalence','area copper and keepouts','via-rule layer IDs','stackup persistence','native DRC','manufacturing acceptance'],
            'captureLimitations':live.get('partial',[]),'completeAcceptance':False}


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--source-inventory',type=Path,required=True);p.add_argument('--live-dump',type=Path,required=True);p.add_argument('--out',type=Path,required=True)
    p.add_argument('--native',type=Path);p.add_argument('--document')
    a=p.parse_args()
    if bool(a.native)!=bool(a.document):p.error('--native and --document must be used together')
    live=json.loads(a.live_dump.read_text());proofs=empty_pad_proofs(a.native,a.document,live['components']) if a.native else {}
    r=compare(json.loads(a.source_inventory.read_text()),live,empty_proofs=proofs)
    if a.native:r['nativeEmptyPadEvidence']={'sha256':hashlib.sha256(a.native.read_bytes()).hexdigest(),'objects':proofs}
    with a.out.open('x') as f:json.dump(r,f,ensure_ascii=False,indent=2)
    print(r['status'],len(r['errors']),'mismatches; completeAcceptance=false')
    return int(bool(r['errors']))
if __name__=='__main__':raise SystemExit(main())
