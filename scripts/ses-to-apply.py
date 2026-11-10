#!/usr/bin/env python3
"""Convert a local two-layer Specctra session into auditable typed Apply steps.
No editor access. Input must be checked against the same board used to route it.
Units are taken from SES, layer IDs and via drills are explicit policy inputs.
"""
import argparse,json,re,hashlib
from pathlib import Path

def parse(text):
    tokens=re.findall(r'"(?:\\.|[^"\\])*"|[()]|[^\s()]+',text);i=0
    def node():
        nonlocal i
        t=tokens[i];i+=1
        if t!='(': return json.loads(t) if t.startswith('"') else t
        out=[]
        while i<len(tokens) and tokens[i]!=')':out.append(node())
        if i==len(tokens):raise ValueError('Unclosed form')
        i+=1;return out
    root=node()
    if i!=len(tokens):raise ValueError('Trailing forms')
    return root

def children(n,k):return [x for x in n if isinstance(x,list) and x and x[0]==k]
def one(n,k):
    x=children(n,k)
    if len(x)!=1:raise ValueError('Expected one '+k)
    return x[0]

def convert(text,policy):
    r=one(parse(text),'routes');res=one(r,'resolution')
    if res[1]!='mil':raise ValueError('Only mil supported')
    scale=float(res[2]);assert scale>0
    stack={}
    for p in children(one(r,'library_out'),'padstack'):
        shapes=[one(s,'circle') for s in children(p,'shape')]
        if {s[1] for s in shapes}!=set(policy['layers']):raise ValueError('Unsupported via span')
        ds={float(s[2])/scale for s in shapes}
        if len(ds)!=1:raise ValueError('Asymmetric via')
        stack[p[1]]=ds.pop()
    steps=[]
    for net in children(one(r,'network_out'),'net'):
        for obj in net[2:]:
            if obj[0]=='wire':
                p=one(obj,'path');layer=policy['layers'][p[1]];w=float(p[2])/scale
                nums=[float(x)/scale for x in p[3:]];assert len(nums)%2==0
                pts=list(zip(nums[::2],nums[1::2]))
                for a,b in zip(pts,pts[1:]):
                    if a==b:continue
                    steps.append({'action':'pcb.line.create','payload':dict(startX=a[0],startY=a[1],endX=b[0],endY=b[1],lineWidth=w,layer=layer,net=net[1])})
            elif obj[0]=='via':
                steps.append({'action':'pcb.via.create','payload':dict(x=float(obj[2])/scale,y=float(obj[3])/scale,diameter=stack[obj[1]],holeDiameter=policy['viaHoleMil'],net=net[1])})
            else:raise ValueError('Unsupported route object '+obj[0])
    for i,s in enumerate(steps):s['id']=f'copper-{i+1:04d}'
    return steps

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('ses');p.add_argument('policy');p.add_argument('out');a=p.parse_args()
    policy=json.loads(Path(a.policy).read_text());text=Path(a.ses).read_text();steps=convert(text,policy)
    plan={'version':1,'meta':{'project':policy['project'],'doc':policy['doc'],'name':'SES typed geometry import'},'defaults':{'retry':0,'continueOnError':False},'steps':steps}
    Path(a.out).write_text(json.dumps(plan,indent=2)+'\n')
    Path(a.out+'.source.json').write_text(json.dumps({'sha256':hashlib.sha256(text.encode()).hexdigest(),'policy':policy,'steps':len(steps)},indent=2))
    print(len(steps),'typed copper steps')
