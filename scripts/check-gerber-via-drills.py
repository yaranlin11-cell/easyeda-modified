#!/usr/bin/env python3
"""Compare full-package via drill hits with native dimensions and layer rules."""
import argparse,collections,json,re,zipfile,hashlib,math
from pathlib import Path

def parse_drill(text):
    tools={};current=None;hits=[];ended=False
    if 'METRIC,LZ,0000.000000' not in text or 'G90' not in text:raise ValueError('unsupported drill units/format')
    for l in text.splitlines():
        l=l.strip()
        if not l or l.startswith(';'):continue
        if ended:raise ValueError('data after end')
        m=re.fullmatch(r'T(\d+)C(\d+(?:\.\d+)?)',l)
        t=re.fullmatch(r'T(\d+)',l)
        xy=re.fullmatch(r'X(-?\d+\.\d+)Y(-?\d+\.\d+)',l)
        if m:
            if m[1] in tools:raise ValueError('duplicate tool')
            tools[m[1]]=float(m[2])
        elif t:
            current=t[1]
            if current not in tools:raise ValueError('undefined tool')
        elif xy:
            if current is None:raise ValueError('no selected tool')
            hits.append((float(xy[1]),float(xy[2]),tools[current]))
        elif l=='M30':ended=True
        elif l not in ('M48','METRIC,LZ,0000.000000','%','G05','G90','M95'):raise ValueError('unsupported drill statement: '+l)
    if not ended:raise ValueError('missing drill EOF')
    return hits

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for k in ('export','vias','rules','out'):p.add_argument('--'+k,required=True,type=Path)
    a=p.parse_args();r=json.loads(getattr(a,'export').read_text());v=json.loads(a.vias.read_text());rules=json.loads(a.rules.read_text())
    if v['result'].get('dimensionSource')!='native-document' or r['result']['scope']!='all':raise ValueError('native vias and full export required')
    if any(r['context'][k]!=v['context'][k] for k in ('projectUuid','documentUuid')):raise ValueError('identity mismatch')
    archive=Path(r['result']['artifactPath']);digest=hashlib.sha256(archive.read_bytes()).hexdigest()
    if not any(x.get('sha256')==digest for x in r['artifacts']):raise ValueError('hash mismatch')
    table=rules['result']['rules']['config']['Physics']['Blind/Buried Via']['blindVia']['table'];pairs={t['name']:(t['startLayer'],t['endLayer']) for t in table}
    def layer(i):return 'Top' if i==1 else 'Bottom' if i==2 else 'Inner'+str(i-14)
    expected=collections.defaultdict(list)
    for via in v['result']['vias']:
        if via['viaType']==0:name='Drill_PTH_Through_Via.DRL'
        elif via['viaType']==1:
            x,y=pairs[via['designRuleBlindViaName']];name='Drill_PTH_'+layer(x)+'_to_'+layer(y)+'.DRL'
        else:raise ValueError('unsupported via type')
        expected[name].append((via['x']*.0254,via['y']*.0254,via['holeDiameter']*.0254))
    reports={}
    with zipfile.ZipFile(archive) as z:
        for name,wanted in expected.items():
            actual=parse_drill(z.read(name).decode());remaining=list(actual);errors=[];maxdelta=0
            for point in wanted:
                # Nearest matching is only a locator; acceptance keeps the 1 nm export quantum.
                if not remaining:errors.append({'missing':point});continue
                i=min(range(len(remaining)),key=lambda i:math.hypot(point[0]-remaining[i][0],point[1]-remaining[i][1]));q=remaining.pop(i);delta=max(abs(x-y) for x,y in zip(point,q));maxdelta=max(maxdelta,delta)
                if delta>1e-6:errors.append({'expected':point,'actual':q,'maxErrorMm':delta})
            reports[name]=dict(status='passed' if not errors and not remaining else 'failed',expected=len(wanted),actual=len(actual),maxErrorMm=maxdelta,errors=errors,extra=remaining)
    result=dict(status='incomplete' if not reports else 'passed' if all(x['status']=='passed' for x in reports.values()) else 'failed',scope='via drill coordinates, diameters and file layer spans only',layers=reports,coordinateToleranceMm=1e-6,completeAcceptance=False)
    with a.out.open('x') as f:json.dump(result,f,indent=2)
    print(result['status'],{k:(v['expected'],len(v['errors']),v['maxErrorMm']) for k,v in reports.items()})
if __name__=='__main__':main()
