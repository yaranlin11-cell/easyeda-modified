import importlib.util, pathlib, unittest
spec=importlib.util.spec_from_file_location('native_check',pathlib.Path(__file__).with_name('check-native-vias.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class ViaComparisonTests(unittest.TestCase):
 def test_native_serialization_is_within_declared_nanometer_tolerance(self):
  v=dict(net='N',pair=[1,15],x=1,y=2,hole=.07,diameter=.15)
  a=dict(v,hole=2.7559*.0254,diameter=5.9055*.0254)
  self.assertEqual(m.compare([v],[a])['status'],'passed')
 def test_sdk_rounding_is_not_accepted(self):
  v=dict(net='N',pair=[1,15],x=1,y=2,hole=.07,diameter=.15)
  self.assertEqual(m.compare([v],[dict(v,hole=2.8*.0254,diameter=6*.0254)])['status'],'failed')
 def test_net_layer_pair_duplicates_and_missing_objects_cannot_pass(self):
  v=dict(net='N',pair=[1,15],x=1,y=2,hole=.07,diameter=.15)
  for a in ([dict(v,net='X')],[dict(v,pair=[1,2])],[v,v],[]):
   self.assertEqual(m.compare([v],a)['status'],'failed')
if __name__=='__main__':unittest.main()
