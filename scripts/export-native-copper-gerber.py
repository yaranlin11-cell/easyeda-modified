#!/usr/bin/env python3
"""Exact-vertex diagnostic FILL export only. No CAD writes or fabrication approval."""
import argparse,json,zipfile,hashlib,collections,importlib.util
from pathlib import Path
from shapely.geometry import Polygon
from shapely.ops import unary_union
from fractured_copper import polygon_parts


def load(path,document):
    spec=importlib.util.spec_from_file_location('native_inventory',Path(__file__).with_name('check-native-copper-persistence.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
    layers=collections.defaultdict(list)
    for ident,v in m.inventory(path,document).items():
        layer=v['layerId']
        if layer not in [1,2,*range(15,45)]:continue
        if v.get('width')!=0:raise ValueError('nonzero FILL stroke unsupported')
        polys=[];outer=None;holes=[]
        for ring in v['path']:
            if len(ring)<9 or ring[2]!='L' or any(not isinstance(x,(int,float)) for x in ring[:2]+ring[3:]):raise ValueError('only straight closed rings supported')
            nums=ring[:2]+ring[3:];pts=[(nums[i]*.0254,nums[i+1]*.0254) for i in range(0,len(nums),2)]
            if pts[0]!=pts[-1]:raise ValueError('open ring')
            poly=Polygon(pts)
            if not poly.is_valid:raise ValueError('invalid native ring')
            if poly.exterior.is_ccw:
                if outer is not None:polys.append(Polygon(outer,holes))
                outer=pts;holes=[]
            else:
                if outer is None:raise ValueError('hole before outer ring')
                holes.append(pts)
        if outer is not None:polys.append(Polygon(outer,holes))
        if any(not p.is_valid for p in polys):raise ValueError('invalid ring grouping')
        geom=unary_union(polys)
        if geom.is_empty:raise ValueError('empty native fill')
        layers[layer].append(geom)
    return {k:unary_union(v) for k,v in layers.items()}

def encode(geom):
    lines=['G04 Native FILL diagnostic only - not fabrication package*','%FSLAX46Y46*%','%MOMM*%']
    def ring(coords,polarity):
        lines.extend(['%LP'+polarity+'*%','G36*'])
        for i,(x,y) in enumerate(coords):lines.append('G01X%dY%dD%s*'%(round(x*1e6),round(y*1e6),'02' if i==0 else '01'))
        lines.append('G37*')
    for poly in sorted(polygon_parts(geom),key=lambda p:Polygon(p.exterior).area,reverse=True):
        ring(poly.exterior.coords,'D')
        for hole in poly.interiors:ring(hole.coords,'C')
    return '\n'.join(lines+['M02*'])+'\n'

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--from',dest='config',required=True,type=Path);p.add_argument('--out',required=True,type=Path);a=p.parse_args();cfg=json.loads(a.config.read_text())
    if set(cfg)!={'native','document'}:raise ValueError('native and document required')
    native=(a.config.parent/cfg['native']).resolve();layers=load(native,cfg['document'])
    if not layers:raise ValueError('no copper FILL layers')
    with zipfile.ZipFile(a.out,'x',zipfile.ZIP_DEFLATED) as z:
        for layer,geom in layers.items():
            name='Gerber_TopLayer.GTL' if layer==1 else 'Gerber_BottomLayer.GBL' if layer==2 else 'Gerber_InnerLayer%d.G%d'%(layer-14,layer-14)
            z.writestr(name,encode(geom))
    print(json.dumps(dict(status='diagnostic-exported',sourceSha256=hashlib.sha256(native.read_bytes()).hexdigest(),sha256=hashlib.sha256(a.out.read_bytes()).hexdigest(),layers=len(layers),completeAcceptance=False)))
if __name__=='__main__':main()
