#!/usr/bin/env python3
"""Read-only comparison of reference cached copper and diagnostic Gerber regions.
Strict subset: mm/4:6 absolute coordinates, dark linear G36/G37 regions only.
Unknown drawing commands are rejected, never silently omitted. Cut-in contours
are normalized with area preservation; this is not a general Gerber validator.
"""
import argparse,collections,hashlib,json,math,re,zipfile
from pathlib import Path
from shapely.geometry import Polygon, LineString, GeometryCollection
from shapely.ops import unary_union
from shapely.validation import make_valid
from fractured_copper import polygon_parts
from kicad_reference import parse,children,one


def region_geometry(points):
    if len(points)<4 or points[0]!=points[-1]:raise ValueError('open or short contour')
    poly=Polygon(points);g=unary_union(polygon_parts(make_valid(poly)))
    if g.is_empty or abs(g.area-poly.area)>1e-8:raise ValueError('ambiguous self-intersecting contour')
    return g


def read_regions(text):
    if '%LPD*%' in text or '%LPC*%' in text:
        # Sequential polarity groups; each group uses the same strict region parser.
        header=[];body=[];groups=[];polarity='D';inside=False
        for line in text.splitlines():
            line=line.strip()
            if line in ('%LPD*%','%LPC*%'):
                if inside:raise ValueError('polarity changed inside region')
                if line[3]!=polarity and body:groups.append((polarity,body));body=[]
                polarity=line[3];continue
            if line=='G36*':inside=True
            if line=='G37*':inside=False
            if line.startswith('%FS') or line.startswith('%MO'):header.append(line)
            elif line!='M02*':body.append(line)
        if not text.rstrip().endswith('M02*') or inside:raise ValueError('incomplete polarity file')
        if body:groups.append((polarity,body))
        result=GeometryCollection();counts=dict(contours=0,zeroLengthSegments=0,nonRegionStrokes=0,strokeGeometryApproximate=False)
        for polarity,body in groups:
            if not any(x=='G36*' for x in body):
                if any(x and not x.startswith('G04 ') for x in body):raise ValueError('unsupported polarity preamble')
                continue
            geometry,stats=read_regions('\n'.join(header+body+['M02*']))
            result=result.union(geometry) if polarity=='D' else result.difference(geometry)
            for key in ('contours','zeroLengthSegments','nonRegionStrokes'):counts[key]+=stats[key]
            counts['strokeGeometryApproximate']|=stats['strokeGeometryApproximate']
        if result.is_empty:raise ValueError('empty polarity image')
        return result,counts
    polygons=[];points=None;inside=False;ended=False;fmt=False;unit=False;count=0;duplicates=0;apertures={};selected=None;cursor=None;strokes=[]
    for raw in text.splitlines():
        line=raw.strip()
        if not line or line.startswith('G04 '):continue
        if ended:raise ValueError('commands after EOF')
        if line=='%FSLAX46Y46*%':
            if fmt or polygons or inside:raise ValueError('duplicate/late format')
            fmt=True;continue
        if line=='%MOMM*%':
            if unit or polygons or inside:raise ValueError('duplicate/late units')
            unit=True;continue
        aperture=re.fullmatch(r'%ADD(\d+)C,(\d+(?:\.\d+)?)\*%',line)
        selection=re.fullmatch(r'G54D(\d+)\*',line)
        if aperture:
            if inside or aperture[1] in apertures:raise ValueError('duplicate/late aperture state')
            width=float(aperture[2])
            if not math.isfinite(width) or width<=0:raise ValueError('invalid aperture')
            apertures[aperture[1]]=width;continue
        if selection:
            if inside or selection[1] not in apertures:raise ValueError('undefined aperture')
            selected=selection[1];continue
        if line=='G75*':
            if inside:raise ValueError('unexpected state change in region')
            continue
        if line=='G36*':
            if inside or not fmt or not unit:raise ValueError('invalid region start')
            inside=True;points=None;continue
        if line=='G37*':
            if not inside or not points:raise ValueError('invalid region end')
            polygons.append(region_geometry(points));count+=1;points=None;inside=False;continue
        if line=='M02*':
            if inside:raise ValueError('unfinished region')
            ended=True;continue
        m=re.fullmatch(r'G01X(-?\d+)Y(-?\d+)D(01|02)\*',line)
        if m and not inside:
            point=(int(m[1])*1e-6,int(m[2])*1e-6)
            if m[3]=='01':
                if cursor is None or selected is None:raise ValueError('draw without position/aperture')
                strokes.append(LineString([cursor,point]).buffer(apertures[selected]/2,resolution=128))
            cursor=point;continue
        if not m or not inside:raise ValueError('unsupported Gerber command: '+line[:100])
        x,y,op=m.groups();p=(int(x)*1e-6,int(y)*1e-6)
        if op=='02':
            if points:polygons.append(region_geometry(points));count+=1
            points=[p]
        else:
            if points is None:raise ValueError('draw before move')
            if points[-1]==p:duplicates+=1
            else:points.append(p)
    if not ended or inside or not polygons:raise ValueError('incomplete/empty Gerber')
    return unary_union(polygons+strokes),dict(contours=count,zeroLengthSegments=duplicates,nonRegionStrokes=len(strokes),strokeGeometryApproximate=bool(strokes))


def reference_layers(board):
    layers=collections.defaultdict(list)
    for zone in children(board,'zone'):
        for cache in children(zone,'filled_polygon'):
            layer=json.loads(one(cache,'layer')[1]);pts=one(cache,'pts')[1:]
            if any(p[0]!='xy' for p in pts):raise ValueError('curved source cache unsupported')
            points=[(float(p[1]),-float(p[2])) for p in pts]
            if points[0]!=points[-1]:points.append(points[0])
            layers[layer].append(region_geometry(points))
    return {k:unary_union(v) for k,v in layers.items()}


def compare(expected,actual):
    # 1 nm bound follows requested Gerber 4:6 mm coordinate quantization.
    eps=1e-6;difference=expected.symmetric_difference(actual).area
    missing=expected.difference(actual.buffer(eps)).area
    extra=actual.difference(expected.buffer(eps)).area
    area_bound=(expected.length+actual.length)*eps+1e-8
    return dict(status='passed' if difference<=area_bound and max(missing,extra)<=1e-8 else 'failed',
        expectedAreaMm2=expected.area,actualAreaMm2=actual.area,symmetricDifferenceMm2=difference,
        missingBeyondQuantizationMm2=missing,extraBeyondQuantizationMm2=extra,coordinateToleranceMm=eps,
        areaBoundMm2=area_bound,expectedHoles=sum(len(p.interiors) for p in polygon_parts(expected)),
        actualHoles=sum(len(p.interiors) for p in polygon_parts(actual)))


def main():
    ap=argparse.ArgumentParser(description=__doc__)
    for name in ('board','export','out'):ap.add_argument('--'+name,type=Path,required=True)
    a=ap.parse_args();response=json.loads(a.export.read_text());r=response['result'];context=response['context']
    if response.get('ok') is not True or r.get('scope')!='fills-only' or r.get('objects')!=['SolidRegion'] or any(not context.get(k) for k in ('projectUuid','documentUuid')):raise ValueError('identity-bound diagnostic export required')
    archive=Path(r['artifactPath']);digest=hashlib.sha256(archive.read_bytes()).hexdigest()
    if not any(x.get('sha256')==digest and x.get('path')==str(archive) for x in response.get('artifacts',[])):raise ValueError('archive evidence hash mismatch')
    source=reference_layers(parse(a.board.read_text()));reports={}
    with zipfile.ZipFile(archive) as z:
        if len(z.namelist())!=len(set(z.namelist())):raise ValueError('duplicate archive entries')
        names={layer:('Gerber_TopLayer.GTL' if layer=='F.Cu' else 'Gerber_BottomLayer.GBL' if layer=='B.Cu' else 'Gerber_InnerLayer'+re.fullmatch(r'In(\d+)\.Cu',layer)[1]+'.G'+re.fullmatch(r'In(\d+)\.Cu',layer)[1]) for layer in source}
        if set(n for n in z.namelist() if not n.endswith('.txt'))!=set(names.values()):raise ValueError('unexpected/missing Gerber layers')
        for layer,name in names.items():
            try:
                actual,stats=read_regions(z.read(name).decode());reports[layer]={**compare(source[layer],actual),**stats}
                if stats['nonRegionStrokes']:
                    reports[layer]['status']='failed'
                    reports[layer]['reason']='fills-only export contains non-region strokes; approximate stroke geometry is diagnostic only'
            except ValueError as e:reports[layer]=dict(status='unsupported',error=str(e))
            print(layer,reports[layer],flush=True)
    report=dict(status='passed' if reports and all(v['status']=='passed' for v in reports.values()) else 'failed',layers=reports,context=context,completeAcceptance=False,
        sourceSha256=hashlib.sha256(a.board.read_bytes()).hexdigest(),gerberSha256=digest,
        unverified=['all-object fabrication package','Gerber robustness/minimum features','electrical connectivity','DRC','automatic reflow','hardware'])
    with a.out.open('x') as f:json.dump(report,f,ensure_ascii=False,indent=2)
    return int(report['status']!='passed')
if __name__=='__main__':raise SystemExit(main())
