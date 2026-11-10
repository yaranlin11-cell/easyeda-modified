#!/usr/bin/env python3
"""Build an EXPERIMENTAL EasyEDA import candidate without deleting area copper.

This is a compatibility transport, not a KiCad format downgrade. Full-board host
import may still crash. Creation is never acceptance; save/reload/readback required.
"""
import argparse
import hashlib
import json
from pathlib import Path
import zipfile
from kicad_reference import *


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source',type=Path,required=True)
    p.add_argument('--output-dir',type=Path,required=True)
    a=p.parse_args();raw=a.source.read_bytes();b=parse(raw.decode('utf-8'))
    before=inventory(b)
    mapping=legacy_net_references(b)
    placements=legacy_footprint_placements(b)
    circles=split_full_circle_keepouts(b)
    pads=sum(rounded_rectangle_pad(p) for f in children(b,'footprint') for p in children(f,'pad'))
    buried=0
    for v in children(b,'via'):
        # Historical KiCad syntax uses blind for both blind and buried spans.
        # Keep the explicit layer pair; do not turn these into through vias.
        if v[1] == 'buried':v[1]='blind';buried+=1
    after=inventory(b)
    assert before['viaLayerPairs']==after['viaLayerPairs']
    assert before['padCount']==after['padCount']
    assert before['filledPolygonPoints']==after['filledPolygonPoints']
    assert before['objects'].get('zone',0)==after['objects'].get('zone',0)
    out=emit(b)+'\n';assert parse(out)==b
    a.output_dir.mkdir(parents=True,exist_ok=False)
    board=a.output_dir/'compatibility.kicad_pcb';board.write_text(out)
    with zipfile.ZipFile(a.output_dir/'compatibility.zip','w',zipfile.ZIP_DEFLATED) as z:z.write(board,board.name)
    report={'status':'candidate-unverified','sourceSha256':hashlib.sha256(raw).hexdigest(),
            'outputSha256':hashlib.sha256(board.read_bytes()).hexdigest(),'before':before,'after':after,
            'netMapping':mapping,'footprintPlacementsAdapted':placements,'roundrectPadsAdapted':pads,
            'fullCircleKeepoutsAdapted':circles,'buriedViaTokensAdapted':buried,'deletedAreaCopper':False,
            'limitations':['Full-board EasyEDA import has not passed; large area-copper inputs have crashed the host.',
                           'Roundrect recognition tolerance is 0.000002 mm, not a manufacturing tolerance.',
                           'All primitive geometry, nets, layer pairs, rules and save/reload state require independent readback.']}
    (a.output_dir/'manifest.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print('candidate-unverified; no copper removed; do not claim design acceptance')
if __name__=='__main__':main()
