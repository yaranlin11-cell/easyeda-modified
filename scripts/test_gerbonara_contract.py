import hashlib,importlib.util,json,sys,tempfile,unittest,zipfile
from pathlib import Path
from unittest.mock import patch
s=importlib.util.spec_from_file_location('parser_contract',Path(__file__).with_name('check-gerbonara-package.py'));m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class ParserContract(unittest.TestCase):
 def test_unavailable_is_not_pass(self):
  with tempfile.TemporaryDirectory() as td:
   p=Path(td)/'a.zip'
   with zipfile.ZipFile(p,'w') as z:z.writestr('Gerber_TopLayer.GTL','M02*')
   r=dict(ok=True,context={'projectUuid':'p','documentUuid':'d'},result={'artifactPath':str(p),'scope':'all'},artifacts=[dict(path=str(p),sha256=hashlib.sha256(p.read_bytes()).hexdigest())])
   with patch.dict(sys.modules,{'gerbonara':None}):out=m.check(r)
   self.assertEqual(out['status'],'incomplete');self.assertEqual(out['parseStatus'],'unavailable');self.assertFalse(out['completeAcceptance'])
   r['artifacts'][0]['sha256']='wrong'
   with self.assertRaisesRegex(ValueError,'hash mismatch'):m.check(r)
