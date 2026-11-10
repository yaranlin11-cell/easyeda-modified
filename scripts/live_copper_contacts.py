#!/usr/bin/env python3
"""Independent physical contacts from typed live geometry; never connect by net labels.
Circle approximations are enclosed by inner/outer polygons. Borderline contacts
remain unresolved rather than being accepted using a clearance tolerance.
"""
import argparse,collections,hashlib,json,math
from pathlib import Path
from shapely.geometry import Point,LineString,Polygon,box
from shapely.affinity import scale,rotate,translate
from shapely.strtree import STRtree
from fractured_copper import polygon_parts

N=64
FACTOR=1/math.cos(math.pi/(4*N))

def disk(x,y,r):
    if not math.isfinite(r) or r<=0:raise ValueError('invalid radius')
    p=Point(x,y)
    return p.buffer(r,quad_segs=N),p.buffer(r*FACTOR,quad_segs=N)

def annulus(x,y,outer,hole):
    if not 0<hole<outer:raise ValueError('invalid annulus')
    a,b=disk(x,y,outer/2);c,d=disk(x,y,hole/2)
    return a.difference(d),b.difference(c)

def track(x1,y1,x2,y2,width):
    if width<=0 or (x1,y1)==(x2,y2):raise ValueError('invalid track')
    l=LineString([(x1,y1),(x2,y2)])
    return l.buffer(width/2,quad_segs=N),l.buffer(width/2*FACTOR,quad_segs=N)

def pad_shape(p):
    if p.get('specialPadPresent') is not False:raise ValueError('special pad absent evidence missing')
    shape=p['shape'];w,h=shape[1:3]
    if min(w,h)<=0:raise ValueError('invalid pad dimensions')
    if shape[0]=='RECT':
        ratio=shape[3] if len(shape)>3 else 0
        if not 0<=ratio<100:raise ValueError('unsupported rectangle radius percentage')
        radius=min(w,h)*ratio/200
        core=box(-w/2+radius,-h/2+radius,w/2-radius,h/2-radius)
        a=core.buffer(radius,quad_segs=N) if radius else core
        b=core.buffer(radius*FACTOR,quad_segs=N) if radius else core
    elif shape[0]=='ELLIPSE':
        a,b=disk(0,0,1);a=scale(a,w/2,h/2,origin=(0,0));b=scale(b,w/2,h/2,origin=(0,0))
    else:raise ValueError('unsupported pad shape')
    uncertainty=p.get('positionUncertaintyMil',0)
    if uncertainty<0 or not math.isfinite(uncertainty):raise ValueError('invalid position uncertainty')
    if uncertainty:a=a.buffer(-uncertainty);b=b.buffer(uncertainty*FACTOR)
    return tuple(translate(rotate(g,p['rotation'],origin=(0,0)),p['x'],p['y']) for g in (a,b))

def fill_parts(f):
    if f['lineWidth']!=0 or not f['geometryAvailable']:raise ValueError('unsupported fill stroke/geometry')
    raw=f['source'];rings=raw if isinstance(raw[0],list) else [raw]
    polys=[];outer=None;holes=[]
    for r in rings:
        if len(r)<9 or r[2]!='L':raise ValueError('unsupported fill ring')
        nums=r[:2]+r[3:]
        if len(nums)%2 or any(not isinstance(n,(float,int)) for n in nums):raise ValueError('curved fill')
        pts=list(zip(nums[::2],nums[1::2]));p=Polygon(pts)
        if pts[0]!=pts[-1] or not p.is_valid:raise ValueError('invalid fill ring')
        if p.exterior.is_ccw:
            if outer is not None:polys.append(Polygon(outer,holes))
            outer=pts;holes=[]
        else:
            if outer is None:raise ValueError('hole without outer')
            holes.append(pts)
    if outer is not None:polys.append(Polygon(outer,holes))
    if any(not p.is_valid for p in polys):raise ValueError('invalid fill grouping')
    return polys

def classify(a,b):
    if not a[1].intersects(b[1]):return 'separate'
    if a[0].intersection(b[0]).area>0:return 'contact'
    return 'unresolved-boundary'

def extract(d,rules,layers):
    ids=[l['id'] for l in layers['layers'] if l['type'] in ('SIGNAL','PLANE') and l['layerStatus']!=0]
    ids=sorted(ids,key=lambda i:999 if i==2 else i)
    if len(ids)!=layers['copperLayerCount']:raise ValueError('layer count mismatch')
    table=rules['rules']['config']['Physics']['Blind/Buried Via']['blindVia']['table']
    pairs={x['name']:(x['startLayer'],x['endLayer']) for x in table}
    items=collections.defaultdict(list);nodes=[];edges=[];missing=[];uncertain=[]
    def add(id,layer,shape,kind,net):
        if layer not in ids:raise ValueError('unknown copper layer')
        if any(g.is_empty or not g.is_valid for g in shape):raise ValueError('invalid geometry')
        key=id+'@'+str(layer);nodes.append(dict(id=key,scope='board',kind=kind,layer=layer,net=net));items[layer].append((key,shape,net));return key
    if d.get('componentsAvailability')!='available':raise ValueError('components unavailable')
    for f in d['components']:
        if f['availability']['pads']!='available':missing.append(dict(object=f['primitiveId'],reason='pads unavailable'));continue
        for p in f['pads']:
            try:
                if not p.get('nativeExact'):raise ValueError(p.get('nativeUnsupported','native pad precision unavailable'))
                shape=pad_shape(p);hole=p.get('nativeHole')
                if hole and hole.get('width',0)>0:
                    if hole['holeType']!='ROUND' or hole['width']!=hole['height']:raise ValueError('slot hole unsupported')
                    inner,outer=disk(p['x'],p['y'],hole['width']/2)
                    shape=(shape[0].difference(outer),shape[1].difference(inner))
                targets=ids if p['layer']==12 else [p['layer']]
                if p['layer']==12 and p.get('nativePlated') is not True:raise ValueError('unplated multilayer pad unsupported')
                previous=None
                for layer in targets:
                    key=add(p['primitiveId'],layer,shape,'pad',p['net'])
                    if previous:edges.append({'from':previous,'to':key,'kind':'pad-barrel','evidence':'live'})
                    previous=key
            except ValueError as e:missing.append(dict(object=p['primitiveId'],reason=str(e)))
    c=d['copper']
    for k in ('routing','vias','fills','poured'):
        if c['availability'].get(k)!='available':raise ValueError(k+' unavailable')
    for p in c['lines']:add(p['primitiveId'],p['layer'],track(p['startX'],p['startY'],p['endX'],p['endY'],p['lineWidth']),'track',p['net'])
    for p in c['arcs']:missing.append(dict(object=p['primitiveId'],reason='arc width unsupported'))
    for p in c['vias']:
        try:
            if not p.get('viaMetadataAvailable'):raise ValueError('via metadata unavailable')
            if p['viaType']==0:pair=(ids[0],ids[-1])
            elif p['viaType']==1:pair=pairs[p['designRuleBlindViaName']]
            else:raise ValueError('unsupported via type')
            lo,hi=sorted(ids.index(i) for i in pair)
            if lo==hi:raise ValueError('zero via span')
            shape=annulus(p['x'],p['y'],p['diameter'],p['holeDiameter']);previous=None
            for layer in ids[lo:hi+1]:
                key=add(p['primitiveId'],layer,shape,'via-annulus',p['net'])
                if previous:edges.append({'from':previous,'to':key,'kind':'barrel','evidence':'live'})
                previous=key
        except (ValueError,KeyError) as e:missing.append(dict(object=p['primitiveId'],reason=str(e)))
    for p in c['fills']:
        if p['layer'] not in ids:continue
        try:
            for i,g in enumerate(fill_parts(p)):add(p['primitiveId']+':'+str(i),p['layer'],(g,g),'fill',p.get('net'))
        except ValueError as e:missing.append(dict(object=p['primitiveId'],reason=str(e)))
    for p in c['poured']:missing.append(dict(object=p.get('primitiveId'),reason='poured geometry unsupported'))
    for layer,rows in items.items():
        tree=STRtree([r[1][1] for r in rows])
        for i,(a,ga,na) in enumerate(rows):
            for j in tree.query(ga[1],predicate='intersects'):
                if j<=i:continue
                b,gb,nb=rows[j];status=classify(ga,gb)
                if status=='contact':edges.append({'from':a,'to':b,'kind':'area-contact','evidence':'live','netLabelsDiffer':na is not None and nb is not None and na!=nb})
                elif status!='separate':uncertain.append({'from':a,'to':b,'reason':status})
    return dict(status='incomplete',scope='physical-contact-evidence-only',schemaVersion=1,complete=False,completeAcceptance=False,coverage=[],nodes=nodes,edges=edges,missing=missing,unresolvedContacts=uncertain,unverified=['module internal connections','hole spacing and stacked via acceptance','manufacturing acceptance','boundary-only contacts'],units='mil',circleSegments=4*N)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for key in ('dump','rules','layers','native','native-vias','out'):p.add_argument('--'+key,required=True,type=Path)
    a=p.parse_args();inputs={k:getattr(a,k) for k in ('dump','rules','layers')};data={k:json.loads(v.read_text()) for k,v in inputs.items()}
    from native_contact_overlay import overlay
    capture=data['dump'].get('capture',{})
    if capture.get('identityVerified') is not True:raise ValueError('verified capture identity required')
    for key in ('rules','layers'):
        receipt=data[key]
        if receipt.get('ok') is not True or receipt.get('windowId')!=capture.get('windowId'):raise ValueError('readback window mismatch')
        if receipt.get('context') and any(receipt['context'].get(k)!=capture.get(k) for k in ('projectUuid','documentUuid')):raise ValueError('readback identity mismatch')
    nv=getattr(a,'native_vias');data['dump']=overlay(data['dump'],a.native,data['dump']['capture']['documentUuid'],json.loads(nv.read_text()));inputs.update(native=a.native,native_vias=nv)
    g=extract(data['dump'],data['rules']['result'],data['layers']['result']);g['sources']=[dict(id=k,path=str(v.resolve()),sha256=hashlib.sha256(v.read_bytes()).hexdigest()) for k,v in inputs.items()];g['capture']=data['dump'].get('capture')
    with a.out.open('x') as f:json.dump(g,f,ensure_ascii=False)
    print(json.dumps({k:len(g[k]) for k in ('nodes','edges','missing','unresolvedContacts')}))
if __name__=='__main__':main()
