#!/usr/bin/env python3
"""Read-only comparison of full-circle reference keepouts to native quarter arcs.
Only circular boundaries, layer assignment and supported rule flags are covered.
"""
import argparse,collections,hashlib,json,math
from pathlib import Path
from kicad_reference import parse,children,one


def circle(source):
    if not isinstance(source,list) or len(source)!=18:return None
    if any(source[i:i+2]!=['ARC',-90] for i in (2,6,10,14)):return None
    points=[source[:2]]+[source[i+2:i+4] for i in (2,6,10,14)]
    if not all(isinstance(v,(int,float)) and math.isfinite(v) for p in points for v in p):raise ValueError('nonfinite circle')
    points=[(p[0]*.0254,p[1]*.0254) for p in points]
    if math.dist(points[0],points[-1])>3e-6:raise ValueError('circle not closed')
    center=tuple((points[0][i]+points[2][i])/2 for i in (0,1));radius=math.dist(points[0],center)
    if radius<=0:raise ValueError('zero radius')
    if any(abs(math.dist(p,center)-radius)>3e-6 for p in points):raise ValueError('inconsistent circular radius')
    for a,b in zip(points,points[1:]):
        # Source stores four clockwise quarter arcs in y-up coordinates.
        if math.dist(b,(center[0]+a[1]-center[1],center[1]-a[0]+center[0]))>3e-6:raise ValueError('not quarter-circle endpoints')
    return center,radius


def compare(board,regions,layers):
    if regions.get('ok') is not True or layers.get('ok') is not True:raise ValueError('live reads failed')
    if any(not isinstance(regions.get('context',{}).get(k),str) or not regions['context'][k] for k in ('projectUuid','documentUuid')):raise ValueError('exact project/document identity required')
    if any(regions['context'].get(k)!=layers['context'].get(k) for k in ('projectUuid','documentUuid')):raise ValueError('document identity mismatch')
    lm={};ls=layers['result']['layers']
    for l in one(board,'layers')[1:]:
        name=json.loads(l[1])
        if not name.endswith('.Cu'):continue
        found=[x for x in ls if (x['id']==1 if name=='F.Cu' else x['id']==2 if name=='B.Cu' else len(l)>3 and x['name']==json.loads(l[3])) and x['type'] in ('SIGNAL','PLANE')]
        if len(found)!=1:raise ValueError('ambiguous layer: '+name)
        lm[name]=found[0]['id']
    expected=[];source_count=0
    for z in children(board,'zone'):
        if not children(z,'keepout'):continue
        rules=dict(x[0:2] for x in one(z,'keepout')[1:])
        for p in children(z,'polygon'):
            pts=one(p,'pts')
            if len(pts)!=2 or pts[1][0]!='arc':continue
            a=pts[1];s=tuple(map(float,one(a,'start')[1:]));m=tuple(map(float,one(a,'mid')[1:]));e=tuple(map(float,one(a,'end')[1:]))
            if s!=e:continue
            if any(rules.get(k)!='allowed' for k in ('vias','pads')):raise ValueError('unsupported pad/via exclusion')
            source_count+=1;c=((s[0]+m[0])/2,-(s[1]+m[1])/2);r=math.dist(s,m)/2
            rule=tuple(sorted(v for k,v in [('tracks',5),('copperpour',7),('footprints',2)] if rules[k]=='not_allowed'))
            for l in (one(z,'layers')[1:] if children(z,'layers') else one(z,'layer')[1:]):expected.append((lm[json.loads(l)],rule,c,r))
    groups=collections.defaultdict(list);ids=set();ignored=0
    for row in regions['result']['regions']:
        if row['primitiveId'] in ids:raise ValueError('duplicate region identity')
        ids.add(row['primitiveId']);c=circle(row.get('source'))
        if c is None:ignored+=1;continue
        if row.get('geometryAvailable') is not True:raise ValueError('unavailable circle geometry')
        groups[(row['layer'],tuple(sorted(row['ruleType'])))].append((row['primitiveId'],*c))
    errors=[];matched=0;maximum=0
    # Spatial bins avoid an O(n^2) scan on dense boards. Search neighbor bins.
    bins=collections.defaultdict(list);cell=.001
    for key,rows in groups.items():
        for row in rows:bins[(*key,math.floor(row[1][0]/cell),math.floor(row[1][1]/cell))].append(row)
    for layer,rules,c,r in expected:
        bx,by=math.floor(c[0]/cell),math.floor(c[1]/cell);found=[]
        for x in range(bx-1,bx+2):
            for y in range(by-1,by+2):
                bucket=bins[(layer,rules,x,y)]
                found.extend((bucket,v) for v in bucket if max(math.dist(v[1],c),abs(v[2]-r))<=3e-6)
        if not found:errors.append(dict(type='missing-circle-or-rule',layer=layer,center=c,radius=r));continue
        bucket,v=found[0];bucket.remove(v);matched+=1;maximum=max(maximum,math.dist(v[1],c),abs(v[2]-r))
    extra=sum(map(len,bins.values()))
    if extra:errors.append(dict(type='unexpected-circles',count=extra))
    return dict(status='passed' if not errors else 'failed',sourceCircularKeepouts=source_count,expectedLayerInstances=len(expected),matched=matched,
                otherRegionsNotChecked=ignored,maxGeometryErrorMm=maximum,toleranceMm=3e-6,errors=errors,completeAcceptance=False,
                unverified=['noncircular keepouts','copper exclusion after reflow','complete copper geometry','DRC','manufacturing acceptance'])


def main():
    p=argparse.ArgumentParser(description=__doc__)
    for arg in ('board','regions','layers','out'):p.add_argument('--'+arg,type=Path,required=True)
    a=p.parse_args();r=compare(parse(a.board.read_text()),json.loads(a.regions.read_text()),json.loads(a.layers.read_text()))
    r['sourceHashes']={k:hashlib.sha256(getattr(a,k).read_bytes()).hexdigest() for k in ('board','regions','layers')}
    with a.out.open('x') as f:json.dump(r,f,ensure_ascii=False,indent=2)
    print(r['status'],r['matched'],'/',r['expectedLayerInstances'],'circle layer instances; completeAcceptance=false')
    return int(r['status']!='passed')
if __name__=='__main__':raise SystemExit(main())
