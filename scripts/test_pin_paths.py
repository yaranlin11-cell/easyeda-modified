import importlib.util,unittest
from pathlib import Path
s=importlib.util.spec_from_file_location('paths',Path(__file__).with_name('check-pin-paths.py'));m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class Paths(unittest.TestCase):
 def fixture(self):
  g={'nodes':[{'id':p+'@1'} for p in ('a','b','c','d')],'edges':[{'from':'a@1','to':'b@1'},{'from':'c@1','to':'d@1'}],'missing':[],'unresolvedContacts':[]}
  d={'components':[{'designator':'J','pads':[{'primitiveId':p,'padNumber':str(i+1),'net':'SAME'} for i,p in enumerate(('a','b','c','d'))]}]}
  return g,d,{'pinGroups':[['J.1','J.2'],['J.3','J.4']]}
 def test_net_names_do_not_connect(self):
  g,d,r=self.fixture();g['edges']=[];self.assertFalse(m.compare(g,d,r)['requiredPathsPassed'])
 def test_missing_geometry_does_not_pass(self):
  g,d,r=self.fixture();g['missing']=['pour'];result=m.compare(g,d,r);self.assertTrue(result['requiredPathsPassed']);self.assertEqual(result['status'],'incomplete')
 def test_short_is_failed(self):
  g,d,r=self.fixture();g['edges'].append({'from':'b@1','to':'c@1'});self.assertEqual(m.compare(g,d,r)['status'],'failed')
 def test_positive_scope_not_whole_acceptance(self):
  result=m.compare(*self.fixture());self.assertEqual(result['status'],'passed');self.assertFalse(result['completeAcceptance'])
