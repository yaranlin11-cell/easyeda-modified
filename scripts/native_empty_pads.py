"""Read-only native-footprint evidence for known-empty pad inventories."""
import json,zipfile,collections

def empty_pad_proofs(path,document,components):
    docs={};current=None
    with zipfile.ZipFile(path) as z:
        streams=[n for n in z.namelist() if n.endswith('.epru')]
        if len(streams)!=1:raise ValueError('one native stream required')
        for line in z.read(streams[0]).decode().splitlines():
            h,v=line.split('||',1);h=json.loads(h);v=json.loads(v.rstrip('|'))
            if h['type']=='DOCHEAD':
                current=v['uuid']
                if current in docs:raise ValueError('duplicate document')
                docs[current]=[]
            if current is not None:docs[current].append((h,v))
    body=docs[document];instances={h['id']:v for h,v in body if h['type']=='COMPONENT'}
    proofs={}
    for c in components:
        ident=c['primitiveId'];instance=instances.get(ident)
        if not instance or any(instance.get(k)!=c.get(s) for k,s in [('x','x'),('y','y'),('angle','rotation')]):continue
        refs=[v['value'] for h,v in body if h['type']=='ATTR' and v.get('parentId')==ident and v.get('key')=='Footprint']
        if len(refs)!=1 or refs[0] not in docs:continue
        rows=docs[refs[0]]
        if any(h['type'] in ('PAD','COMPONENT') for h,v in rows):continue
        # A real, populated footprint document, not a missing/empty library.
        head=rows[0][1]
        if head.get('docType')!='FOOTPRINT' or not any(h['type'] in ('POLY','STRING') for h,v in rows):continue
        proofs[ident]={'footprint':refs[0],'padCount':0,'basis':'native-footprint-records','document':document}
    return proofs
