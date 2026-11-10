#!/usr/bin/env python3
"""Check explicitly required pin groups against physical graph edges, never net labels."""
import argparse,json,hashlib
from pathlib import Path

def compare(graph,dump,requirements):
    groups=requirements['pinGroups']
    if not groups or any(len(x)<2 for x in groups):raise ValueError('explicit nonempty pin groups required')
    flat=[pin for group in groups for pin in group]
    if len(set(flat))!=len(flat):raise ValueError('pin appears in multiple expected groups')
    ids={n['id'] for n in graph['nodes']};parent={i:i for i in ids}
    def root(x):
        while parent[x]!=x:parent[x]=parent[parent[x]];x=parent[x]
        return x
    for e in graph['edges']:
        if e['from'] not in ids or e['to'] not in ids:raise ValueError('edge endpoint absent')
        parent[root(e['from'])]=root(e['to'])
    pins={}
    for c in dump['components']:
        for p in c.get('pads') or []:
            key=c['designator']+'.'+p['padNumber']
            if key in pins:raise ValueError('duplicate pin identity')
            pins[key]={root(i) for i in ids if i.startswith(p['primitiveId']+'@')}
    reports=[]
    for group in groups:
        sets=[pins.get(p,set()) for p in group];reports.append(dict(pins=group,connected=bool(set.intersection(*sets))))
    shorts=[]
    for i,a in enumerate(groups):
        for b in groups[i+1:]:
            for x in a:
                for y in b:
                    if pins.get(x,set()) & pins.get(y,set()):shorts.append([x,y])
    passed=all(r['connected'] for r in reports)
    status='failed' if shorts else 'incomplete' if not passed or graph.get('missing') or graph.get('unresolvedContacts') else 'passed'
    return dict(status=status,scope='explicit-pin-paths-only',requiredPathsPassed=passed,groups=reports,unexpectedConnections=shorts,unverified=graph.get('missing',[]),completeAcceptance=False)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for k in ('graph','dump','requirements','out'):p.add_argument('--'+k,type=Path,required=True)
    a=p.parse_args();graph,dump,requirements=[json.loads(getattr(a,k).read_text()) for k in ('graph','dump','requirements')]
    if not any(x.get('id')=='dump' and x.get('sha256')==hashlib.sha256(a.dump.read_bytes()).hexdigest() for x in graph.get('sources',[])):raise ValueError('graph is not bound to this dump')
    if any(graph.get('capture',{}).get(k)!=dump.get('capture',{}).get(k) or not dump.get('capture',{}).get(k) for k in ('projectUuid','documentUuid')):raise ValueError('capture identity mismatch')
    r=compare(graph,dump,requirements)
    with a.out.open('x') as f:json.dump(r,f,indent=2)
    print(r['status'],'requiredPathsPassed='+str(r['requiredPathsPassed']));return int(r['status']!='passed')
if __name__=='__main__':raise SystemExit(main())
