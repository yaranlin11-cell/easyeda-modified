import importlib.util,json,os,pathlib,tempfile,unittest
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('runner',pathlib.Path(__file__).with_name('runner.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class EvidenceTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.base=pathlib.Path(self.tmp.name);self.run=self.base/'run'
  self.source=self.base/'source.txt';self.source.write_text('independent requirement')
  self.candidate=self.base/'candidate.txt';self.candidate.write_text('candidate')
  self.receipt=self.base/'receipt.json';self.receipt.write_text(json.dumps({'ok':False,'error':{'code':'DISPATCH_FAILED'}}))
  self.config=self.base/'config.json';self.config.write_text(json.dumps(dict(mode='reference-migration',requirements=[str(self.source)],inputs=[str(self.source)],requiredChecks=['reference'])))
  m.init({'from':str(self.config),'out':str(self.run)})
  self.t=self.base/'trial.json';self.payload=dict(id='y1',workflowVersion='x1',verifierVersion='v1',hypothesis='test evidence',candidate=str(self.candidate),evidence=[str(self.receipt)],evidenceKind='historical')
  self.record()
 def record(self):self.t.write_text(json.dumps(self.payload));return m.record({'run':str(self.run),'from':str(self.t)})
 def test_baseline_copy_is_independent(self):
  self.source.write_text('later change');self.assertEqual(m.verify({'run':str(self.run)})['status'],'incomplete')
 def test_no_overwrite_and_no_pending_on_failure(self):
  with self.assertRaises(ValueError):self.record()
  self.payload['id']='y2';self.payload['candidate']='missing'
  with self.assertRaises(OSError):self.record()
  self.assertEqual([x.name for x in (self.run/'trials').iterdir()],['y1'])
 def test_tampered_evidence_rejected(self):
  (self.run/'trials/y1/evidence0.json').write_text('{}')
  with self.assertRaisesRegex(ValueError,'hash mismatch'):m.verify({'run':str(self.run)})
 def test_tampered_manifest_rejected(self):
  p=self.run/'baseline.json';p.write_text(p.read_text().replace('reference-migration','independent-design'))
  with self.assertRaises(ValueError):m.verify({'run':str(self.run)})
 def test_unknown_fields_and_duplicate_json_rejected(self):
  self.payload['extra']=1
  with self.assertRaises(ValueError):self.record()
  self.config.write_text('{"x":1,"x":2}')
  with self.assertRaises(ValueError):m.read(self.config)
 def test_lock_and_partial_transaction_rejected(self):
  with m.lock(self.run):
   with self.assertRaises(FileExistsError):self.record()
   with self.assertRaises(ValueError):m.verify({'run':str(self.run)})
  (self.run/'trials/.pending-dead').mkdir()
  with self.assertRaises(ValueError):m.verify({'run':str(self.run)})
 def test_path_escape_and_symlink_rejected(self):
  for p in ['../candidate.txt',str(self.candidate)]:
   with self.assertRaises(ValueError):m.safe(self.run,p)
  (self.run/'link').symlink_to(self.source)
  with self.assertRaises(ValueError):m.safe(self.run,'link')
 def test_wrong_project_rejected(self):
  self.receipt.write_text(json.dumps(dict(context=dict(projectUuid='other',documentUuid='d'))))
  self.payload.update(id='y2',projectUuid='p',documentUuid='d')
  with self.assertRaisesRegex(ValueError,'another project'):self.record()
 def test_diagnosis_does_not_claim_root_cause_or_write(self):
  before={str(p):p.read_bytes() for p in self.run.rglob('*') if p.is_file()}
  r=m.diagnose({'run':str(self.run),'trial':'y1'});self.assertFalse(r['rootCauseConfirmed']);self.assertEqual(r['findings'][0]['category'],'connection')
  self.assertEqual(before,{str(p):p.read_bytes() for p in self.run.rglob('*') if p.is_file()})
 def test_repeat_triggers_review_not_root_cause(self):
  self.payload.update(id='y2',parent='y1');self.record();r=m.diagnose({'run':str(self.run),'trial':'y2'})
  self.assertTrue(r['strategyReviewSuggested']);self.assertFalse(r['rootCauseConfirmed'])
 def test_unbound_checker_input_refused(self):
  p=self.base/'inputs.json';p.write_text(json.dumps({'from':'outside.json'}))
  with self.assertRaises((OSError,ValueError)):m.check({'run':str(self.run),'trial':'y1','checker':'reference','inputs':str(p)})
 def test_uncaptured_dependency_refused(self):
  p=self.base/'input.json';p.write_text(json.dumps({'sources':[{'path':str(self.source)}]}))
  with self.assertRaises(ValueError):m.dependencies({'from':p},{})
 def test_failed_or_missing_report_cannot_pass(self):
  # Use fixed spec adapter with a mocked child process, not arbitrary executable input.
  p=self.base/'inputs.json';p.write_text(json.dumps({'from':'trials/y1/evidence0.json'}))
  class Proc:returncode=1
  def fail(cmd,stdout,stderr,**kwargs):stdout.write(b'{"status":"pass"}');return Proc()
  with patch.dict(os.environ,{'EASYEDA_EXPERIMENT_CLI':str(self.source)}),patch.object(m.subprocess,'run',side_effect=fail):
   r=m.check({'run':str(self.run),'trial':'y1','checker':'reference','inputs':str(p)});self.assertEqual(r['status'],'incomplete')
  self.assertEqual(m.verify({'run':str(self.run)})['status'],'incomplete')
 def test_completed_checker_is_bound_and_artifact_tampering_detected(self):
  p=self.base/'inputs.json';p.write_text(json.dumps({'from':'trials/y1/evidence0.json'}))
  class Proc:returncode=0
  def success(cmd,stdout,stderr,**kwargs):stdout.write(b'{"status":"pass","scope":"offline"}');return Proc()
  with patch.dict(os.environ,{'EASYEDA_EXPERIMENT_CLI':str(self.source)}),patch.object(m.subprocess,'run',side_effect=success):m.check({'run':str(self.run),'trial':'y1','checker':'reference','inputs':str(p)})
  r=m.verify({'run':str(self.run)});self.assertEqual(r['status'],'pass');self.assertFalse(r['completeAcceptance'])
  (self.run/'trials/y1/checks/reference/result.json').write_text('{}')
  with self.assertRaises(ValueError):m.verify({'run':str(self.run)})
 def test_malformed_error_is_not_a_root_cause(self):
  self.receipt.write_text('{"ok":false,"error":"broken response"}')
  self.payload['id']='y2';self.record()
  result=m.diagnose({'run':str(self.run),'trial':'y2'})
  self.assertEqual(result['findings'][0]['observedCode'],'unknown')
  self.assertFalse(result['rootCauseConfirmed'])
 def test_nonobject_report_and_timeout_are_incomplete(self):
  p=self.base/'inputs.json';p.write_text(json.dumps({'from':'trials/y1/evidence0.json'}))
  class Proc:returncode=0
  def malformed(cmd,stdout,stderr,**kwargs):stdout.write(b'[]');return Proc()
  for index,behavior in enumerate([malformed,m.subprocess.TimeoutExpired('checker',120)]):
   self.payload['id']='malformed'+str(index);self.record()
   p.write_text(json.dumps({'from':'trials/'+self.payload['id']+'/evidence0.json'}))
   with patch.dict(os.environ,{'EASYEDA_EXPERIMENT_CLI':str(self.source)}),patch.object(m.subprocess,'run',side_effect=behavior):
    result=m.check({'run':str(self.run),'trial':self.payload['id'],'checker':'reference','inputs':str(p)})
   self.assertEqual(result['status'],'incomplete')
   self.assertEqual(result['timedOut'],index==1)
 def test_partial_pass_is_checker_specific(self):
  p=self.base/'inputs.json';p.write_text(json.dumps({'from':'trials/y1/evidence0.json'}))
  class Proc:returncode=0
  def partial(cmd,stdout,stderr,**kwargs):stdout.write(b'{"status":"partial-check-passed"}');return Proc()
  with patch.dict(os.environ,{'EASYEDA_EXPERIMENT_CLI':str(self.source)}),patch.object(m.subprocess,'run',side_effect=partial):
   result=m.check({'run':str(self.run),'trial':'y1','checker':'reference','inputs':str(p)})
  self.assertEqual(result['status'],'incomplete')
if __name__=='__main__':unittest.main()
