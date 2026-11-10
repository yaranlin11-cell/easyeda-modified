"""Explicit native geometry overlay, preserving the original SDK observation."""
import copy,json,math,zipfile,importlib.util
from pathlib import Path

def overlay(d,path,document,native_vias):
    d=copy.deepcopy(d);docs={};cur=None
    with zipfile.ZipFile(path) as z:
        names=[n for n in z.namelist() if n.endswith('.epru')]
        if len(names)!=1:raise ValueError('ambiguous native stream')
        for l in z.read(names[0]).decode().splitlines():
            h,v=l.split('||',1);h=json.loads(h);payload=v.rstrip('|')
            if not payload:
                if cur is not None:docs[cur]=[(a,b) for a,b in docs[cur] if a.get('id')!=h.get('id')]
                continue
            v=json.loads(payload)
            if h['type']=='DOCHEAD':cur=v['uuid'];docs[cur]=[]
            docs[cur].append((h,v))
    b=docs[document];instances={h['id']:v for h,v in b if h['type']=='COMPONENT'}
    refs={v['parentId']:v['value'] for h,v in b if h['type']=='ATTR' and v.get('key')=='Footprint'}
    for f in d['components']:
        ins=instances[f['primitiveId']]
        if any(abs(ins[k]-f[t])>2*max(math.ulp(ins[k]),math.ulp(f[t])) for k,t in [('x','x'),('y','y'),('angle','rotation')]):raise ValueError('instance differs from live readback')
        pads={h['id']:v for h,v in docs[refs[f['primitiveId']]] if h['type']=='PAD'}
        if not pads and not any(h['type']=='COMPONENT' for h,v in docs[refs[f['primitiveId']]]):
            f['pads']=[];f['availability']['pads']='available';continue
        for p in f.get('pads') or []:
            q=pads.get(p['primitiveId'][len(f['primitiveId']):])
            if q is None:raise ValueError('native pad identity missing')
            if ins['layerId'] not in (1,2) or ((q['padOffsetX'] or q['padOffsetY']) and q.get('hole') and q['hole'].get('width',0)>0) or q['specialPad'] or q.get('unusedInnerLayers'):
                p['nativeUnsupported']='bottom/mirrored or offset/special pad';continue
            a=math.radians(ins['angle']);local_y=q['centerY']*(-1 if ins['layerId']==2 else 1);x=q['centerX']*math.cos(a)-local_y*math.sin(a)+ins['x'];y=q['centerX']*math.sin(a)+local_y*math.cos(a)+ins['y']
            if math.hypot(x-p['x'],y-p['y'])>.1:raise ValueError('native pad transform inconsistent with SDK position')
            p.update(x=x,y=y,rotation=q['padAngle']*(-1 if ins['layerId']==2 else 1)+ins['angle'],shape=[q['defaultPad']['padType'],q['defaultPad']['width'],q['defaultPad']['height']]+([q['defaultPad']['radius']] if q['defaultPad']['padType']=='RECT' else []),nativeHole=q['hole'],nativePlated=q['plated'],nativeExact=True,positionUncertaintyMil=math.hypot(q['padOffsetX'],q['padOffsetY']))
    if native_vias['result'].get('dimensionSource')!='native-document':raise ValueError('native via dimensions required')
    if native_vias['context']['documentUuid']!=document or native_vias['context']['projectUuid']!=d['capture']['projectUuid']:raise ValueError('via identity mismatch')
    a={p['primitiveId'] for p in d['copper']['vias']};v=native_vias['result']['vias']
    if a!={p['primitiveId'] for p in v}:raise ValueError('via inventory changed')
    d['copper']['vias']=[dict(p,viaMetadataAvailable=True) for p in v]
    fills={h['id']:v for h,v in b if h['type']=='FILL'}
    for p in d['copper']['fills']:
        q=fills[p['primitiveId']]
        if q['layerId']!=p['layer']:raise ValueError('fill layer changed')
        p.update(source=q['path'],lineWidth=q['width'])
    native_lines={h['id']:v for h,v in b if h['type']=='LINE'}
    for p in d['copper']['lines']:
        q=native_lines[p['primitiveId']]
        if any(abs(p[k]-q[t])>2*max(math.ulp(p[k]),math.ulp(q[t])) for k,t in [('startX','startX'),('startY','startY'),('endX','endX'),('endY','endY'),('lineWidth','width')]):raise ValueError('native track differs from SDK')
    d['nativeGeometryOverlay']=True
    return d
