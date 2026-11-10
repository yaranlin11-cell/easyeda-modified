#!/usr/bin/env python3
"""Extract a partial track/arc endpoint and via barrel graph; never infer connectivity from nets."""
import argparse,hashlib,json,math
from decimal import Decimal
from pathlib import Path
from kicad_reference import parse,children,one

def extract(board,source):
    nodes={};edges=[]
    def endpoint(layer,point):
        xy=[Decimal(x) for x in point[1:]]
        if len(xy)!=2 or not all(x.is_finite() for x in xy):raise ValueError('invalid endpoint')
        key=layer+':'+','.join(str(x.normalize()) if x else '0' for x in xy)
        nodes[key]={'id':key,'scope':'board'}
        return key
    for i,s in enumerate(children(board,'segment')):
        layer=json.loads(one(s,'layer')[1]);width=float(one(s,'width')[1])
        if not math.isfinite(width) or width<=0:raise ValueError('invalid track width')
        a=endpoint(layer,one(s,'start'));b=endpoint(layer,one(s,'end'))
        if a==b:raise ValueError('zero length track')
        edges.append(dict(id='segment-'+str(i),**{'from':a,'to':b},kind='board-copper',evidence='board'))
    for i,s in enumerate(children(board,'arc')):
        layer=json.loads(one(s,'layer')[1])
        a=endpoint(layer,one(s,'start'));b=endpoint(layer,one(s,'end'))
        mid=endpoint(layer,one(s,'mid'))
        if a==b:raise ValueError('closed arc needs explicit geometry support')
        edges.append(dict(id='arc-'+str(i)+'-a',**{'from':a,'to':mid},kind='board-copper',evidence='board'))
        edges.append(dict(id='arc-'+str(i)+'-b',**{'from':mid,'to':b},kind='board-copper',evidence='board'))
    if children(board,'via'):
        copper=[json.loads(row[1]) for row in one(board,'layers')[1:] if json.loads(row[1]).endswith('.Cu')]
        for i,v in enumerate(children(board,'via')):
            pair=[json.loads(x) for x in one(v,'layers')[1:]]
            if len(pair)!=2 or any(x not in copper for x in pair):raise ValueError('unknown via layer pair')
            start,end=sorted(copper.index(x) for x in pair)
            if start==end:raise ValueError('zero span via')
            at=one(v,'at')
            # Barrel joins only the declared layer span, never all layers by net name.
            for j in range(start,end):
                a=endpoint(copper[j],at);b=endpoint(copper[j+1],at)
                edges.append(dict(id='via-'+str(i)+'-'+str(j),**{'from':a,'to':b},kind='board-copper',evidence='board'))
    for fi,f in enumerate(children(board,'footprint')):
        if not children(f,'pad'):continue
        transform=one(f,'transform');translate=one(transform,'translate')[1:];angle=Decimal(one(transform,'rotate')[1])
        if one(transform,'scale')[1:]!=['1','1'] or angle%90:raise ValueError('only unit scale and quadrant footprint transforms supported')
        def rotated(x,y,degrees):
            for _ in range(int(degrees/90)%4):x,y=y,-x
            return x,y
        properties={json.loads(x[1]):json.loads(x[2]) for x in children(f,'property')}
        for pi,pad in enumerate(children(f,'pad')):
            at=one(pad,'at');x,y=rotated(Decimal(at[1]),Decimal(at[2]),angle);x+=Decimal(translate[0]);y+=Decimal(translate[1])
            ident='pad:'+str(fi)+':'+str(pi)+':'+properties.get('Reference','')+':'+json.loads(pad[1]);nodes[ident]={'id':ident,'scope':'board'}
            layers=[json.loads(x) for x in one(pad,'layers')[1:]]
            if '*.Cu' in layers:layers=[json.loads(row[1]) for row in one(board,'layers')[1:] if json.loads(row[1]).endswith('.Cu')]
            if pad[2]=='np_thru_hole':continue
            for layer in layers:
                if not layer.endswith('.Cu'):continue
                target=endpoint(layer,['at',str(x),str(y)])
                edges.append(dict(id='pad-center-'+str(fi)+'-'+str(pi)+'-'+layer,**{'from':ident,'to':target},kind='board-copper',evidence='board'))
    if children(board,'zone'):
        from shapely.geometry import Polygon,Point
        from shapely.prepared import prep
        from shapely.validation import make_valid
        # Cached copper only; point membership is a conservative subset of contacts.
        point_nodes=list(nodes)
        for zi,z in enumerate(children(board,'zone')):
            for ci,cache in enumerate(children(z,'filled_polygon')):
                layer=json.loads(one(cache,'layer')[1]);pts=one(cache,'pts')[1:]
                if any(p[0]!='xy' for p in pts):raise ValueError('curved cached copper unsupported')
                geom=make_valid(Polygon([(float(p[1]),float(p[2])) for p in pts]))
                from fractured_copper import polygon_parts
                for partno,poly in enumerate(polygon_parts(geom)):
                    ident='copper:'+str(zi)+':'+str(ci)+':'+str(partno);nodes[ident]={'id':ident,'scope':'board'}
                    prepared=prep(poly)
                    for target in point_nodes:
                        if not target.startswith(layer+':'):continue
                        x,y=map(float,target.split(':',1)[1].split(','))
                        if prepared.covers(Point(x,y)):
                            edges.append(dict(id='contact-'+str(len(edges)),**{'from':ident,'to':target},kind='board-copper',evidence='board'))
    return dict(schemaVersion=1,complete=False,coverage=[],sources=[source],nodes=list(nodes.values()),edges=edges)

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--board',required=True,type=Path);p.add_argument('--out',required=True,type=Path);a=p.parse_args()
    raw=a.board.read_bytes();source=dict(id='board',path=str(a.board.resolve()),sha256=hashlib.sha256(raw).hexdigest())
    graph=extract(parse(raw.decode()),source)
    with a.out.open('x') as f:json.dump(graph,f,indent=2)
    print(json.dumps(dict(status='incomplete',graph=str(a.out),nodes=len(graph['nodes']),edges=len(graph['edges']),completeAcceptance=False,unverified=['track crossings and width contact','pad boundary/width contacts beyond center anchors','arc/track width contacts','via annulus contacts','copper boundary contacts beyond captured point membership','module internal connections','solder joints','independent chain intent'])))
if __name__=='__main__':main()
