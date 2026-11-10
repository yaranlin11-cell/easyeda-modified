#!/usr/bin/env python3
"""Check cached copper FILL fields in a fresh native export after save/reload.

Requires the converter's candidate archive and manifest, not an SDK rounded dump.
Exact path equality is deliberate: any host normalization needs separate geometry
analysis, never a silently widened tolerance. This is not DRC/manufacturing proof.
"""
import argparse,hashlib,json,zipfile
from pathlib import Path


def inventory(path,target):
 with zipfile.ZipFile(path) as z:
  files=[n for n in z.namelist() if n.endswith('.epru')]
  if len(files)!=1:raise ValueError('exactly one native stream required')
  text=z.read(files[0]).decode()
 rows={};active=False;heads=0
 for line in text.splitlines():
  a,b=line.split('||',1);h=json.loads(a);v=json.loads(b[:-1] if b.endswith('|') else b)
  if h['type']=='DOCHEAD':
   active=v.get('uuid')==target and v.get('docType')=='PCB'
   if active:heads+=1
  if active and h['type']=='FILL':
   if h['id'] in rows:raise ValueError('duplicate FILL ID')
   rows[h['id']]=v
 if heads!=1:raise ValueError('wrong or ambiguous target PCB')
 return rows


def compare(expected,actual,manifest):
 errors=[];matched=0
 if set(expected)!=set(actual):errors.append(dict(type='fill-inventory-changed',missing=sorted(set(expected)-set(actual)),unexpected=sorted(set(actual)-set(expected))))
 keys=('layerId','netName','width','fillStyle','path','locked','isBridgingCopper')
 ids=[v['primitiveId'] for v in manifest['fillObjects']]
 if not ids or len(ids)!=len(set(ids)):raise ValueError('empty or duplicate expected IDs')
 for ident in ids:
  if ident not in expected:raise ValueError('manifest does not match candidate')
  e=expected[ident];a=actual.get(ident)
  if e.get('width')!=0 or not isinstance(e.get('netName'),str) or not e['netName']:raise ValueError('invalid expected copper fields')
  differences=['missing'] if a is None else [k for k in keys if k not in a or a[k]!=e[k]]
  if differences:errors.append(dict(primitiveId=ident,differences=differences))
  else:matched+=1
 return dict(status='passed' if not errors else 'failed',expected=len(ids),matched=matched,
             preservedHoleCount=sum(v['holes'] for v in manifest['fillObjects']) if not errors else None,
             fields=list(keys),comparison='exact native values',errors=errors,completeAcceptance=False,
             unverified=['manufacturing Gerber geometry','electrical connectivity','DRC','editable pour reflow semantics'])


def main():
 p=argparse.ArgumentParser(description=__doc__)
 for k in ('candidate','manifest','reloaded','out'):p.add_argument('--'+k,type=Path,required=True)
 a=p.parse_args();m=json.loads(a.manifest.read_text())
 if hashlib.sha256(a.candidate.read_bytes()).hexdigest()!=m['outputSha256']:raise ValueError('candidate SHA mismatch')
 result=compare(inventory(a.candidate,m['targetDocument']),inventory(a.reloaded,m['targetDocument']),m)
 result['sources']={k:hashlib.sha256(getattr(a,k).read_bytes()).hexdigest() for k in ('candidate','manifest','reloaded')}
 with a.out.open('x') as f:json.dump(result,f,ensure_ascii=False,indent=2)
 print(result['status'],result['matched'],'/',result['expected'],'native FILL objects; completeAcceptance=false')
 return int(result['status']!='passed')
if __name__=='__main__':raise SystemExit(main())
