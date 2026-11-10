"""Offline experiment evidence store. No CAD writes or arbitrary command execution."""
import argparse, contextlib, datetime, hashlib, json, os, re, shutil, subprocess, sys, tempfile
from pathlib import Path

SCHEMA=1
SCRIPTS={
 'pin-paths':('check-pin-paths.py',('graph','dump','requirements')),
 'live-contacts':('live_copper_contacts.py',('dump','rules','layers','native','native-vias')),
 'gerber-package':('audit-full-gerber.py',('export',)),
 'gerber-via-drills':('check-gerber-via-drills.py',('export','vias','rules')),
 'gerbonara-parser':('check-gerbonara-package.py',('export',)),
 'native-vias':('check-native-vias.py',('board','native','rules','layers')),
 'circle-keepouts':('check-circle-keepouts.py',('board','regions','layers')),
 'copper-persistence':('check-native-copper-persistence.py',('candidate','manifest','reloaded')),
 'copper-gerber':('check-copper-gerber.py',('board','export')),
 'live-reference':('check-kicad-live-reference.py',('source-inventory','live-dump')),
}
SPECS={'reference':('reference-check',('from',)), 'chains':('chain-check',('from','observed')), 'constraints':('constraints-check',('from','observed'))}

def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def digest(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def pairs(xs):
 d={}
 for k,v in xs:
  if k in d:raise ValueError('duplicate JSON key: '+k)
  d[k]=v
 return d
def read(p):return json.loads(Path(p).read_text(),object_pairs_hook=pairs,parse_constant=lambda x:(_ for _ in ()).throw(ValueError('nonfinite JSON')))
def fields(d,required,optional=()):
 if not isinstance(d,dict) or set(d)-set(required)-set(optional) or set(required)-set(d):raise ValueError('missing or unknown fields: '+str(required))
def ident(s):
 if not isinstance(s,str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,79}',s):raise ValueError('invalid identifier')
 return s
def nonempty(s):
 if not isinstance(s,str) or not s.strip():raise ValueError('nonempty string required')
 return s
def write(p,v):
 with Path(p).open('x') as f:json.dump(v,f,ensure_ascii=False,indent=2,allow_nan=False);f.write('\n');f.flush();os.fsync(f.fileno())
 with Path(str(p)+'.sha256').open('x') as f:f.write(digest(p));f.flush();os.fsync(f.fileno())
def safe(root,rel):
 if not isinstance(rel,str) or Path(rel).is_absolute() or '..' in Path(rel).parts:raise ValueError('unsafe evidence path')
 p=root/rel
 if any(x.is_symlink() for x in [p,*p.parents] if x!=root.parent):raise ValueError('symlink evidence refused')
 if not p.resolve().is_relative_to(root.resolve()):raise ValueError('evidence escapes run')
 return p
def copy_file(src,dst):
 src=Path(src).resolve(strict=True)
 if not src.is_file():raise ValueError('regular file required')
 before=digest(src);shutil.copyfile(src,dst)
 if digest(src)!=before or digest(dst)!=before:raise ValueError('source changed during copy')
 return dict(path=str(dst.name),sha256=before,source=str(src))
@contextlib.contextmanager
def lock(root):
 p=root/'.lock';fd=os.open(p,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 try:
  os.write(fd,str(os.getpid()).encode());os.close(fd);yield
 finally:p.unlink()
def commit_dir(parent,name,fn):
 if (parent/name).exists():raise ValueError('refusing to overwrite '+name)
 temp=Path(tempfile.mkdtemp(prefix='.pending-',dir=parent))
 try:
  fn(temp);os.rename(temp,parent/name)
 except BaseException:shutil.rmtree(temp);raise

def sealed(p):
 if digest(p)!=Path(str(p)+'.sha256').read_text():raise ValueError('record hash mismatch: '+str(p))
 return read(p)

def load_run(root):
 m=sealed(root/'baseline.json');fields(m,('schemaVersion','createdAt','mode','requirements','inputs','requiredChecks'))
 if m['schemaVersion']!=SCHEMA:raise ValueError('unsupported schema')
 for collection in ('requirements','inputs'):
  for row in m[collection]:
   if digest(safe(root,row['path']))!=row['sha256']:raise ValueError('baseline hash mismatch: '+row['path'])
 return m

def init(a):
 cfg=read(a['from']);fields(cfg,('mode','requirements','inputs','requiredChecks'))
 if cfg['mode'] not in ('reference-migration','independent-design'):raise ValueError('invalid mode')
 if any(not isinstance(cfg[k],list) or not cfg[k] for k in ('requirements','inputs','requiredChecks')):raise ValueError('nonempty inputs, requirements and checks required')
 if any(not isinstance(x,str) or x not in set(SCRIPTS)|set(SPECS) for x in cfg['requiredChecks']) or len(set(cfg['requiredChecks']))!=len(cfg['requiredChecks']):raise ValueError('unknown or duplicate required checker')
 out=Path(a['out']).absolute();out.parent.mkdir(parents=True,exist_ok=True)
 def build(t):
  m=dict(schemaVersion=SCHEMA,createdAt=now(),mode=cfg['mode'],requiredChecks=cfg['requiredChecks'])
  base=Path(a['from']).resolve().parent
  for kind in ('requirements','inputs'):
   m[kind]=[]
   for i,p in enumerate(cfg[kind]):
    row=copy_file(base/nonempty(p),t/(kind+str(i)+Path(p).suffix));m[kind].append(row)
  write(t/'baseline.json',m);(t/'trials').mkdir()
 commit_dir(out.parent,out.name,build)
 return dict(status='baseline-frozen',run=str(out),completeAcceptance=False)

def record(a):
 root=Path(a['run']).resolve();load_run(root);cfg=read(a['from'])
 fields(cfg,('id','workflowVersion','verifierVersion','hypothesis','candidate','evidence','evidenceKind'),('parent','projectUuid','documentUuid'))
 ident(cfg['id'])
 if cfg['evidenceKind'] not in ('historical','fresh-offline','live-capture'):raise ValueError('explicit evidenceKind required')
 if bool(cfg.get('projectUuid'))!=bool(cfg.get('documentUuid')):raise ValueError('both project and document identity required')
 for k in ('workflowVersion','verifierVersion','hypothesis'):nonempty(cfg[k])
 if not isinstance(cfg['evidence'],list) or not cfg['evidence']:raise ValueError('evidence list required')
 with lock(root):
  load_run(root)
  if cfg.get('parent') and not (root/'trials'/ident(cfg['parent'])/'trial.json').is_file():raise ValueError('unknown parent')
  def build(t):
   base=Path(a['from']).resolve().parent
   files=[copy_file(base/nonempty(cfg['candidate']),t/('candidate'+Path(cfg['candidate']).suffix))]
   for i,p in enumerate(cfg['evidence']):files.append(copy_file(base/nonempty(p),t/('evidence'+str(i)+Path(p).suffix)))
   row={k:v for k,v in cfg.items() if k not in ('candidate','evidence')}
   row.update(schemaVersion=SCHEMA,createdAt=now(),baselineSha256=digest(root/'baseline.json'),files=files)
   if cfg.get('projectUuid'):
    for f in files[1:]:
     try: envelope=read(t/f['path'])
     except (ValueError,OSError,UnicodeError):continue
     context=envelope.get('context') if isinstance(envelope,dict) else None
     if isinstance(context,dict) and any(context.get(k)!=cfg[k] for k in ('projectUuid','documentUuid')):raise ValueError('evidence belongs to another project/document')
   write(t/'trial.json',row);(t/'checks').mkdir()
  commit_dir(root/'trials',cfg['id'],build)
 return dict(status='recorded',trial=cfg['id'],completeAcceptance=False)

def trial(root,id):
 load_run(root);p=root/'trials'/ident(id);r=sealed(p/'trial.json')
 if r['id']!=id or r['baselineSha256']!=digest(root/'baseline.json'):raise ValueError('trial identity/baseline mismatch')
 for f in r['files']:
  if digest(safe(p,f['path']))!=f['sha256']:raise ValueError('trial evidence hash mismatch')
 return p,r

def dependencies(paths,registered):
 observed={}
 for path in paths.values():
  if path.suffix.lower()!='.json':continue
  v=read(path)
  refs=[]
  if isinstance(v,dict):
   sources=v.get('sources',[])
   if isinstance(sources,list):
    refs.extend(x['path'] for x in sources if isinstance(x,dict) and 'path' in x)
   result=v.get('result',{})
   if isinstance(result,dict) and 'artifactPath' in result:refs.append(result['artifactPath'])
  for ref in refs:
   q=(path.parent/nonempty(ref)).resolve(strict=True)
   if str(q) not in registered or digest(q)!=registered[str(q)]:raise ValueError('uncaptured or relocated dependency: '+str(q))
   observed[str(q)]=digest(q)
 return observed

def check(a):
 root=Path(a['run']).resolve();p,r=trial(root,a['trial']);name=a['checker'];mapping=read(a['inputs'])
 if name not in SCRIPTS and name not in SPECS:raise ValueError('unknown checker')
 keys=(SCRIPTS.get(name) or SPECS[name])[1];fields(mapping,keys)
 # Inputs must refer to immutable files already recorded under this run.
 allowed={str((root/f['path']).resolve()):f['sha256'] for kind in ('inputs','requirements') for f in load_run(root)[kind]}
 allowed.update({str((p/f['path']).resolve()):f['sha256'] for f in r['files']})
 paths={k:safe(root,mapping[k]).resolve(strict=True) for k in keys}
 if any(str(v) not in allowed or digest(v)!=allowed[str(v)] for v in paths.values()):raise ValueError('checker input is not bound to this trial/baseline')
 registered={f['source']:f['sha256'] for kind in ('inputs','requirements') for f in load_run(root)[kind]}
 registered.update({f['source']:f['sha256'] for f in r['files']})
 execution=dict(paths)
 captured=[(root,f) for kind in ('inputs','requirements') for f in load_run(root)[kind]]+[(p,f) for f in r['files']]
 originals={str((base/f['path']).resolve()):Path(f['source']) for base,f in captured}
 for key,value in paths.items():
  original=originals[str(value)]
  if original.is_file() and digest(original)==allowed[str(value)]:execution[key]=original
 deps=dependencies(execution,registered)
 with lock(root):
  trial(root,a['trial'])
  def build(t):
   output=t/'result.json';code_hash={};exe=Path(os.environ['EASYEDA_EXPERIMENT_CLI']).resolve()
   if name in SPECS:
    cmd=[str(exe),'spec',SPECS[name][0]];code_hash[str(exe)]=digest(exe)
   else:
    repo=Path(a.get('repo') or '').resolve();script=repo/'scripts'/SCRIPTS[name][0]
    if not script.is_file():raise ValueError('--repo with registered checker scripts required')
    # Pin all Python helpers, not just the wrapper.
    code_hash={str(x):digest(x) for x in (repo/'scripts').glob('*.py')}
    cmd=[sys.executable,str(script),'--out',str(output)]
   for k,v in execution.items():cmd+=['--'+k,str(v)]
   try:
    with (t/'stdout.txt').open('xb') as out,(t/'stderr.txt').open('xb') as err:
     proc=subprocess.run(cmd,stdout=out,stderr=err,timeout=120,check=False)
    exit_code=proc.returncode;timed_out=False
   except subprocess.TimeoutExpired:exit_code=None;timed_out=True
   if name in SPECS:shutil.copyfile(t/'stdout.txt',output)
   status='incomplete';raw=None
   try:
    raw=read(output);native=raw.get('status') if isinstance(raw,dict) else None
    if native in ('fail','failed'):status='fail'
    elif exit_code==0 and (native in ('pass','passed') or (name=='live-reference' and native=='partial-check-passed')):status='pass'
   except (ValueError,OSError,UnicodeError):pass
   if any(digest(execution[k])!=allowed[str(v)] for k,v in paths.items()) or any(digest(k)!=v for k,v in deps.items()) or any(digest(v)!=allowed[str(v)] for v in paths.values()) or any(digest(k)!=v for k,v in code_hash.items()):raise ValueError('checker/input changed during execution')
   artifacts=[dict(path=x.name,sha256=digest(x)) for x in t.iterdir() if x.is_file()]
   write(t/'check.json',dict(schemaVersion=SCHEMA,checker=name,status=status,exitCode=exit_code,timedOut=timed_out,createdAt=now(),trialSha256=digest(p/'trial.json'),inputs={k:dict(path=str(v.relative_to(root)),sha256=digest(v)) for k,v in paths.items()},implementation=code_hash,executionPaths={k:str(v) for k,v in execution.items()},dependencies=deps,python=sys.version,artifacts=artifacts,evidenceKind=r['evidenceKind'],scope='offline-checker-only',reportedStatus=raw.get('status') if isinstance(raw,dict) else None,reportedScope=raw.get('scope') if isinstance(raw,dict) else None,limitations=raw.get('unverified',[]) if isinstance(raw,dict) else [],completeAcceptance=False))
  commit_dir(p/'checks',name,build)
 return read(p/'checks'/name/'check.json')

def verify(a):
 root=Path(a['run']).resolve();m=load_run(root);rows=[]
 if any(x.name.startswith('.pending-') for x in root.iterdir()):raise ValueError('unfinished run transaction')
 for p in sorted((root/'trials').iterdir()):
  if not p.is_dir() or p.name.startswith('.'):raise ValueError('unfinished or unexpected trial entry')
  _,r=trial(root,p.name);checks={}
  for q in (p/'checks').iterdir():
   if q.name.startswith('.'):raise ValueError('unfinished checker transaction')
   c=sealed(q/'check.json')
   if c['checker']!=q.name or c['trialSha256']!=digest(p/'trial.json'):raise ValueError('check identity mismatch')
   for f in c['artifacts']:
    if digest(safe(q,f['path']))!=f['sha256']:raise ValueError('checker artifact changed')
   for f in c['inputs'].values():
    if digest(safe(root,f['path']))!=f['sha256']:raise ValueError('checker input changed')
   if q.name not in set(SCRIPTS)|set(SPECS) or c['status'] not in ('pass','fail','incomplete'):raise ValueError('invalid checker record')
   checks[q.name]=c['status']
  missing=[x for x in m['requiredChecks'] if x not in checks]
  status='fail' if 'fail' in checks.values() else 'incomplete' if missing or 'incomplete' in checks.values() else 'pass'
  rows.append(dict(id=p.name,evidenceKind=r['evidenceKind'],status=status,checks=checks,missing=missing))
 if (root/'.lock').exists():raise ValueError('run locked; concurrent or interrupted writer')
 return dict(status='fail' if any(x['status']=='fail' for x in rows) else 'pass' if rows and all(x['status']=='pass' for x in rows) else 'incomplete',trials=rows,scope='record-integrity-and-declared-offline-checks',completeAcceptance=False)

def diagnose(a):
 root=Path(a['run']).resolve();verification=verify(a);p,r=trial(root,a['trial']);findings=[]
 for f in r['files'][1:]:
  try:v=read(p/f['path'])
  except (ValueError,OSError,UnicodeError):continue
  if not isinstance(v,dict):continue
  if v.get('ok') is False:
   e=v.get('error');code=e.get('code','unknown') if isinstance(e,dict) else 'unknown'
   category='connection' if code in ('NO_CONNECTOR','DISPATCH_FAILED','TIMEOUT') else 'tool-or-precondition'
   findings.append(dict(category=category,evidence=f['path'],observedCode=code,cause='undetermined',nextCheck='Read fresh health and active identity; inspect transport logs before retry. Do not infer a crash or replay writes.' if category=='connection' else 'Inspect original error detail and the affected object readback.'))
  result=v.get('result',{})
  if isinstance(result,dict) and 'saved' in result and result['saved'] is not True:
   findings.append(dict(category='persistence',evidence=f['path'],cause='save-not-confirmed',nextCheck='Preserve current state and obtain boolean saved:true before reload.'))
 for row in verification['trials']:
  if row['id']==r['id']:
   for name,status in row['checks'].items():
    if status!='pass':findings.append(dict(category='verification' if status=='fail' else 'missing-evidence',evidence='checks/'+name+'/check.json',cause='undetermined',nextCheck='Inspect raw checker result and limitations; do not relax requirements.'))
   for name in row['missing']:findings.append(dict(category='missing-evidence',evidence='baseline.json',cause='required-check-not-run',checker=name,nextCheck='Run this required checker with trial-bound inputs.'))
 repeated=[]
 for other in verification['trials']:
  if other['id']==r['id']:continue
  op,orr=trial(root,other['id'])
  for f in orr['files'][1:]:
   try: value=read(op/f['path'])
   except (ValueError,OSError,UnicodeError):continue
   if isinstance(value,dict) and value.get('ok') is False and isinstance(value.get('error'),dict):
    code=value['error'].get('code')
    if any(x.get('observedCode')==code for x in findings):repeated.append(dict(trial=other['id'],code=code))
 return dict(status='diagnosed',repeatedObservations=repeated,strategyReviewSuggested=bool(repeated),trial=r['id'],findings=findings,rootCauseConfirmed=False,automaticWrites=False,completeAcceptance=False)

def extract(a):
 repo=Path(a['repo']).resolve();script=repo/'scripts/extract-track-endpoints.py'
 if not script.is_file():raise ValueError('source checkout required')
 proc=subprocess.run([sys.executable,str(script),'--board',a['from'],'--out',a['out']],capture_output=True,text=True,timeout=120)
 if proc.returncode:raise ValueError(proc.stderr)
 return json.loads(proc.stdout)

def export_copper(a):
 script=Path(a['repo']).resolve()/'scripts/export-native-copper-gerber.py'
 if not script.is_file():raise ValueError('source checkout required')
 proc=subprocess.run([sys.executable,str(script),'--from',a['from'],'--out',a['out']],capture_output=True,text=True,timeout=120)
 if proc.returncode:raise ValueError(proc.stderr)
 return json.loads(proc.stdout)

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['init','record','check','verify','diagnose','extract','export-copper'])
 for k in ('from','out','run','trial','checker','inputs','repo'):p.add_argument('--'+k)
 a=vars(p.parse_args());required={'export-copper':['from','out','repo'],'extract':['from','out','repo'],'init':['from','out'],'record':['run','from'],'check':['run','trial','checker','inputs'],'verify':['run'],'diagnose':['run','trial']}[a['action']]
 try:
  if any(not a[k] for k in required):raise ValueError('required: '+','.join(required))
  result=globals()[a['action'].replace('-','_')](a);print(json.dumps(result,ensure_ascii=False,indent=2));return 1 if result['status'] in ('fail','incomplete') else 0
 except (ValueError,OSError,KeyError,TypeError,subprocess.TimeoutExpired) as e:print(json.dumps(dict(status='error',error=str(e),completeAcceptance=False),ensure_ascii=False));return 2
if __name__=='__main__':sys.exit(main())
