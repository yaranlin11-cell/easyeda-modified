#!/usr/bin/env python3
"""Bounded third-party parser compatibility check, not geometry/manufacturing approval."""
import argparse,hashlib,importlib.metadata,json,tempfile,warnings,zipfile
from pathlib import Path

def check(receipt):
    r=receipt['result'];archive=Path(r['artifactPath']);sha=hashlib.sha256(archive.read_bytes()).hexdigest()
    if receipt.get('ok') is not True or r.get('scope')!='all':raise ValueError('full official export required')
    if not all(receipt.get('context',{}).get(k) for k in ('projectUuid','documentUuid')):raise ValueError('export identity missing')
    if not any(x.get('sha256')==sha and x.get('path')==str(archive) for x in receipt['artifacts']):raise ValueError('archive hash mismatch')
    report=dict(status='incomplete',scope='parser-compatibility-only',completeAcceptance=False,archiveSha256=sha,files=[],unverified=['geometry equivalence','DRC','manufacturing acceptance'])
    try:
        from gerbonara import GerberFile,ExcellonFile
        from gerbonara.utils import MM
        report['parserVersion']=importlib.metadata.version('gerbonara')
    except ImportError as e:
        report.update(parseStatus='unavailable',reason='gerbonara dependency unavailable',detail=str(e));return report
    with zipfile.ZipFile(archive) as z,tempfile.TemporaryDirectory() as td:
        if len(z.namelist())!=len(set(z.namelist())) or z.testzip():raise ValueError('invalid archive')
        if sum(x.file_size for x in z.infolist())>100*1024*1024:raise ValueError('expanded archive exceeds 100 MiB')
        for i,name in enumerate(z.namelist()):
            if name.endswith('/') or name.lower().endswith(('.txt','.json')):continue
            row=dict(name=name)
            try:
                path=Path(td)/str(i);path.write_bytes(z.read(name))
                with warnings.catch_warnings(record=True) as observed:
                    warnings.simplefilter('always')
                    model=(ExcellonFile if name.lower().endswith('.drl') else GerberFile).open(path)
                    count=0
                    for obj in model.objects:
                        # Forces aperture-macro evaluation instead of merely accepting syntax.
                        for primitive in obj.to_primitives(unit=MM):count+=1
                    row.update(objects=len(model.objects),primitives=count,warnings=[str(x.message) for x in observed])
                row['status']='parsed-with-warnings' if row['warnings'] else 'parsed'
            except Exception as e:row.update(status='failed',reason=type(e).__name__+': '+str(e))
            report['files'].append(row)
    report['parseStatus']='failed' if any(x['status']=='failed' for x in report['files']) else 'parsed-with-warnings' if any(x['status']=='parsed-with-warnings' for x in report['files']) else 'parsed'
    if report['parseStatus']=='failed':report['status']='failed'
    # Parsing success deliberately remains incomplete for design acceptance.
    return report

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--export',dest='source',required=True,type=Path);p.add_argument('--out',required=True,type=Path);a=p.parse_args();r=check(json.loads(a.source.read_text()))
    with a.out.open('x') as f:json.dump(r,f,ensure_ascii=False,indent=2)
    print(r['status'],r.get('parseStatus'));return 1 if r['status']=='failed' else 0
if __name__=='__main__':raise SystemExit(main())
