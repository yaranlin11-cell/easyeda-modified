#!/usr/bin/env python3
"""Partition large cached copper rings; preserve all zone definitions and other objects."""
import argparse,copy,json,hashlib,zipfile
from pathlib import Path
from shapely.geometry import Polygon
from shapely.ops import unary_union
from kicad_reference import parse,emit,children,one
from fractured_copper import partition

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--source',type=Path,required=True);p.add_argument('--out',type=Path,required=True);a=p.parse_args()
 raw=a.source.read_bytes();b=parse(raw.decode());reports=[]
 for z in children(b,'zone'):
  for f in list(children(z,'filled_polygon')):
   pts=one(f,'pts')[1:]
   if len(pts)<1000:continue
   if any(q[0]!='xy' for q in pts):raise ValueError('curved cache cannot be partitioned by this adapter')
   pieces,r=partition([tuple(map(float,q[1:])) for q in pts]);replacements=[];rounded=[]
   for piece in pieces:
    xy=[['xy',format(x,'.12f'),format(y,'.12f')] for x,y in list(piece.exterior.coords)[:-1]]
    rr=Polygon([(float(q[1]),float(q[2])) for q in xy]);rounded.append(rr)
    item=[copy.deepcopy(x) for x in f if x is not one(f,'pts')];item.append(['pts',*xy]);replacements.append(item)
   difference=unary_union(pieces).symmetric_difference(unary_union(rounded)).area
   if difference>1e-7:raise ValueError('serialization changes copper beyond numeric tolerance')
   r.update(layer=one(f,'layer')[1],serializationDifferenceMm2=difference);reports.append(r);print(r,flush=True)
   i=z.index(f);z[i:i+1]=replacements
 out=emit(b)+'\n';assert parse(out)==b
 a.out.mkdir(parents=True,exist_ok=False);board=a.out/'partitioned.kicad_pcb';board.write_text(out)
 with zipfile.ZipFile(a.out/'partitioned.zip','w',zipfile.ZIP_DEFLATED) as archive:archive.write(board,board.name)
 (a.out/'manifest.json').write_text(json.dumps({'status':'offline-geometry-verified-host-unverified','inputSha256':hashlib.sha256(raw).hexdigest(),'outputSha256':hashlib.sha256(board.read_bytes()).hexdigest(),'numericAreaToleranceMm2':1e-7,'partitions':reports},indent=2))
 print('Partitioned',len(reports),'cached rings; host import unverified')
if __name__=='__main__':main()
