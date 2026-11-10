#!/usr/bin/env python3
"""EXPERIMENTAL offline native-project copper transport; never writes a live page.

Use only a previously verified native baseline of the SAME reference PCB.
Replace its imported copper FILL/POUR objects with static geometry from the
reference's existing filled_polygon caches. All other source records and library
entries remain byte-identical. Import to a NEW project via typed project import.
Official format: https://prodocs.lceda.cn/cn/format/pcb/shape/
This preserves cached geometry, not editable zone/reflow semantics. A candidate
is not accepted until save/reload, native geometry, nets, layer and DRC checks.
"""
import argparse,hashlib,json,math,zipfile
from pathlib import Path
from shapely.geometry import Polygon
from shapely.geometry.polygon import orient
from shapely.affinity import scale
from shapely.validation import make_valid
from shapely.ops import unary_union
from fractured_copper import polygon_parts
from kicad_reference import parse,children,one


def records(text):
    rows=[]
    for line in text.splitlines(keepends=True):
        if '||' not in line:raise ValueError('unsupported native framing')
        h,v=line.rstrip('\r\n').split('||',1)
        header=json.loads(h);payload=json.loads(v[:-1] if v.endswith('|') else v)
        if not isinstance(header,dict) or not isinstance(payload,dict):raise ValueError('unsupported native record')
        rows.append((header,payload,line))
    return rows


def copper_paths(points):
    if len(points)<3 or any(len(p)!=2 or not all(map(math.isfinite,p)) for p in points):raise ValueError('finite polygon coordinates required')
    original=Polygon(points);normalized=unary_union(polygon_parts(make_valid(original)))
    if normalized.is_empty or abs(original.area-normalized.area)>1e-7:raise ValueError('normalization changes filled area')
    paths=[];rebuilt=[]
    # Native format uses nonzero winding; convert y-down/mm to y-up/mil first.
    for poly in polygon_parts(scale(normalized,xfact=1/.0254,yfact=-1/.0254,origin=(0,0))):
        poly=orient(poly,sign=1.0);rings=[poly.exterior,*poly.interiors];rounded=[]
        for ring in rings:
            coords=[(round(x,9),round(y,9)) for x,y in ring.coords]
            rounded.append(coords);paths.append([*coords[0],'L',*[v for xy in coords[1:] for v in xy]])
        rebuilt.append(Polygon(rounded[0],rounded[1:]))
    restored=scale(unary_union(rebuilt),xfact=.0254,yfact=-.0254,origin=(0,0))
    error=normalized.symmetric_difference(restored).area
    if error>1e-7 or not restored.is_valid:raise ValueError('serialization changes copper geometry')
    return paths,dict(areaMm2=normalized.area,serializationDifferenceMm2=error,
                      contours=len(paths),holes=sum(len(p.interiors) for p in polygon_parts(normalized)),
                      polygons=len(polygon_parts(normalized)))


def compose(baseline,reference,target,limit=None):
    with zipfile.ZipFile(baseline) as z:
        if len(z.namelist())!=len(set(z.namelist())):raise ValueError('duplicate archive names')
        native=[n for n in z.namelist() if n.endswith('.epru')]
        if len(native)!=1:raise ValueError('one native project document stream required')
        entries={n:z.read(n) for n in z.namelist()}
    rows=records(entries[native[0]].decode());heads=[(i,h,v) for i,(h,v,_) in enumerate(rows) if h.get('type')=='DOCHEAD']
    hits=[(i,h,v) for i,h,v in heads if v.get('docType')=='PCB' and v.get('uuid')==target]
    if len(hits)!=1:raise ValueError('target PCB is missing or ambiguous')
    start=hits[0][0];end=next((i for i,h,v in heads if i>start),len(rows));body=rows[start:end]
    ids=[h['id'] for h,v,l in body if 'id' in h]
    if len(ids)!=len(set(ids)):raise ValueError('duplicate native target IDs')
    def layer_id(h):
        key=json.loads(h['id'])
        if not isinstance(key,list) or len(key)!=2 or key[0]!='LAYER' or not isinstance(key[1],int):raise ValueError('invalid native layer ID')
        return key[1]
    native_layer_rows=[(h,v) for h,v,l in body if h['type']=='LAYER' and v.get('use') is True]
    mapping={}
    for layer in one(reference,'layers')[1:]:
        name=json.loads(layer[1])
        if not name.endswith('.Cu'):continue
        expected_id=1 if name=='F.Cu' else 2 if name=='B.Cu' else None
        candidates=[(h,v) for h,v in native_layer_rows if (v.get('layerName')==json.loads(layer[3]) if expected_id is None and len(layer)>3 else layer_id(h)==expected_id)]
        if len(candidates)!=1:raise ValueError('missing/ambiguous native layer mapping '+name)
        h,v=candidates[0];ident=layer_id(h)
        if not isinstance(ident,int):raise ValueError('numeric native layer ID required')
        mapping[name]=ident
    copper_ids=set(mapping.values());removed=[];preserved=[]
    for h,v,line in body:
        if h['type'] in ('FILL','POUR','POURED') and v.get('layerId') in copper_ids:
            if not isinstance(v.get('netName'),str) or not v['netName']:raise ValueError('netless copper needs explicit reconciliation')
            removed.append((h,v,line))
        else:preserved.append((h,v,line))
    source_netmap={n[1]:json.loads(n[2]) for n in children(reference,'net') if len(n)==3}
    caches=[]
    for zone in children(reference,'zone'):
        for cache in children(zone,'filled_polygon'):
            net=one(zone,'net')[1];net=json.loads(net) if net.startswith('"') else source_netmap[net]
            layer=mapping[json.loads(one(cache,'layer')[1])];pts=one(cache,'pts')[1:]
            if not net or any(p[0]!='xy' for p in pts):raise ValueError('unsupported netless/curved copper cache')
            caches.append((net,layer,pts))
    if not caches:raise ValueError('no authoritative filled caches')
    # Smallest first permits bounded pilot imports using genuine reference data.
    caches.sort(key=lambda v:len(v[2]));all_count=len(caches)
    if limit is not None:
        if limit<1:raise ValueError('positive pilot cache count required')
        caches=caches[:limit]
    ticket=max(h.get('ticket',0) for h,v,l in body)+1
    zindex=max(float(v['zIndex']) for h,v,l in body if isinstance(v.get('zIndex'),(int,float)))+1
    additions=[];manifest=[];existing=set(ids)
    for index,(net,layer,pts) in enumerate(caches):
        paths,proof=copper_paths([tuple(map(float,p[1:])) for p in pts])
        ident=hashlib.sha256(json.dumps([target,net,layer,paths],separators=(',',':')).encode()).hexdigest()[:16]
        if ident in existing:raise ValueError('generated ID collision')
        existing.add(ident)
        h=dict(type='FILL',ticket=ticket+index,id=ident)
        v=dict(layerId=layer,groupId='',locked=False,zIndex=zindex+index,partitionId=None,netName=net,
               width=0,fillStyle='SOLID',path=paths,isBridgingCopper=False,specialColor=None)
        line=json.dumps(h,separators=(',',':'))+'||'+json.dumps(v,separators=(',',':'))+'|\n';additions.append(line)
        manifest.append(dict(primitiveId=ident,net=net,layer=layer,widthMil=0,**proof))
    kept=''.join(l for h,v,l in preserved)
    # Keep all unmodified record bytes exactly, only adding a newline at the new record boundary if necessary.
    block=kept+('' if kept.endswith('\n') else '\n')+''.join(additions)
    result=''.join(l for h,v,l in rows[:start])+block+''.join(l for h,v,l in rows[end:])
    actual_records=records(result);actual_kept=[l for h,v,l in actual_records if h.get('id') not in {m['primitiveId'] for m in manifest}]
    expected_kept=[l for i,(h,v,l) in enumerate(rows) if not (start<=i<end and (h,v,l) in removed)]
    if ''.join(actual_kept)!=(''.join(expected_kept)):raise ValueError('untargeted record bytes changed')
    entries[native[0]]=result.encode()
    report=dict(status='candidate-unverified' if len(caches)==all_count else 'diagnostic-partial-copper',
        targetDocument=target,baselineSha256=hashlib.sha256(Path(baseline).read_bytes()).hexdigest(),
        availableCaches=all_count,transportedCaches=len(caches),removedCopperObjects=len(removed),
        preservedRecordBytesVerified=True,fillObjects=manifest,completeAcceptance=False,
        semantics='static cached copper; editable zone/reflow semantics are not preserved',
        requirements=['native width=0 readback','net/layer and complex-hole geometry after save/reload','DRC','manufacturing acceptance'])
    return entries,report


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--baseline',type=Path,required=True);p.add_argument('--reference',type=Path,required=True);p.add_argument('--document',required=True);p.add_argument('--out',type=Path,required=True);p.add_argument('--pilot-caches',type=int)
    a=p.parse_args();entries,report=compose(a.baseline,parse(a.reference.read_text()),a.document,a.pilot_caches)
    report['referenceSha256']=hashlib.sha256(a.reference.read_bytes()).hexdigest()
    a.out.mkdir(parents=True,exist_ok=False)
    with zipfile.ZipFile(a.out/'candidate.epro2','x',zipfile.ZIP_DEFLATED) as z:
        for name,data in entries.items():z.writestr(name,data)
    report['outputSha256']=hashlib.sha256((a.out/'candidate.epro2').read_bytes()).hexdigest()
    (a.out/'manifest.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(report['status'],report['transportedCaches'],'cached copper objects')
if __name__=='__main__':main()
