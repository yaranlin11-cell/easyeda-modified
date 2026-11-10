import copy,importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('copper_persistence',pathlib.Path(__file__).with_name('check-native-copper-persistence.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class NativeCopperPersistenceTests(unittest.TestCase):
 def inputs(self):
  v={'layerId':1,'netName':'GND','width':0,'fillStyle':'SOLID','path':[[0,0,'L',1,0,1,1,0,0]],'locked':False,'isBridgingCopper':False}
  return {'f':v},{'fillObjects':[{'primitiveId':'f','holes':0}]}
 def test_exact_native_values_pass(self):
  e,manifest=self.inputs();self.assertEqual(m.compare(e,copy.deepcopy(e),manifest)['status'],'passed')
 def test_changed_width_geometry_net_or_layer_is_rejected(self):
  for k,v in [('width',.2),('netName','N'),('layerId',2),('path',[[0,0,'L',2,0,1,1,0,0]])]:
   e,manifest=self.inputs();a=copy.deepcopy(e);a['f'][k]=v
   self.assertEqual(m.compare(e,a,manifest)['status'],'failed')
 def test_extra_or_missing_copper_cannot_pass(self):
  e,manifest=self.inputs()
  self.assertEqual(m.compare(e,{},manifest)['status'],'failed')
  self.assertEqual(m.compare(e,{**e,'extra':e['f']},manifest)['status'],'failed')
if __name__=='__main__':unittest.main()
