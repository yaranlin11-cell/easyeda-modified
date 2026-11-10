#!/usr/bin/env python3
"""Inventory/hash audit of a full official package; not geometry acceptance."""
import argparse,hashlib,json,re,zipfile
from pathlib import Path

def audit(response):
    r=response['result']
    if response.get('ok') is not True or r.get('scope')!='all':raise ValueError('full official export required')
    if not all(response.get('context',{}).get(k) for k in ('projectUuid','documentUuid')):raise ValueError('export identity missing')
    path=Path(r['artifactPath']);digest=hashlib.sha256(path.read_bytes()).hexdigest()
    if not any(a.get('sha256')==digest and a.get('path')==str(path) for a in response['artifacts']):raise ValueError('artifact hash mismatch')
    files=[]
    with zipfile.ZipFile(path) as z:
        if len(z.namelist())!=len(set(z.namelist())) or z.testzip():raise ValueError('invalid ZIP')
        for n in z.namelist():
            raw=z.read(n);txt=raw.decode('utf-8');files.append(dict(name=n,bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest(),regionStarts=txt.count('G36*'),flashes=len(re.findall(r'D03\*',txt)),apertureMacro='%AM' in txt,arcCommands=len(re.findall(r'G0?[23](?:X|Y|I|J)',txt))))
    return dict(status='incomplete',archiveIntegrity='passed',context=response['context'],sha256=digest,files=files,completeAcceptance=False,unverified=['full copper union versus native geometry','aperture macros and flashes','drill diameter/layer spans and slot geometry','mask and paste expansion','board outline','DRC and manufacturing rules'])

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--export',dest='source',type=Path,required=True);p.add_argument('--out',type=Path,required=True);a=p.parse_args();r=audit(json.loads(a.source.read_text()))
    with a.out.open('x') as f:json.dump(r,f,ensure_ascii=False,indent=2)
    print('incomplete: archive integrity passed;',len(r['files']),'files; geometry not certified')
if __name__=='__main__':main()
