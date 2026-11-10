import copy, importlib.util, pathlib, unittest
spec=importlib.util.spec_from_file_location('circle_check',pathlib.Path(__file__).with_name('check-circle-keepouts.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class CircleReadbackTests(unittest.TestCase):
 def inputs(self):
  b=['kicad_pcb',['layers',['0','"F.Cu"','signal']],['zone',['layer','"F.Cu"'],['keepout',['tracks','not_allowed'],['vias','allowed'],['pads','allowed'],['copperpour','not_allowed'],['footprints','allowed']],['polygon',['pts',['arc',['start','0.0762','0.0254'],['mid','-0.0254','0.0254'],['end','0.0762','0.0254']]]]]]
  source=[3,-1,'ARC',-90,1,-3,'ARC',-90,-1,-1,'ARC',-90,1,1,'ARC',-90,3,-1]
  context={'projectUuid':'p','documentUuid':'d'}
  r={'ok':True,'context':context,'result':{'regions':[{'primitiveId':'r','source':source,'geometryAvailable':True,'layer':1,'ruleType':[5,7]}]}}
  l={'ok':True,'context':context,'result':{'layers':[{'id':1,'name':'Top','type':'SIGNAL'}]}}
  return b,r,l
 def test_independent_circle_reference_matches_four_arc_source(self):
  r=m.compare(*self.inputs());self.assertEqual(r['matched'],1);self.assertEqual(r['status'],'passed')
 def test_rule_loss_or_wrong_layer_rejected(self):
  for k,v in [('layer',2),('ruleType',[5])]:
   b,r,l=self.inputs();r['result']['regions'][0][k]=v
   self.assertEqual(m.compare(b,r,l)['status'],'failed')
 def test_bad_radius_or_wrong_target_rejected(self):
  b,r,l=self.inputs();r['result']['regions'][0]['source'][4]+=1
  with self.assertRaises(ValueError):m.compare(b,r,l)
  b,r,l=self.inputs();r['context']={'projectUuid':'other','documentUuid':'d'}
  with self.assertRaises(ValueError):m.compare(b,r,l)
if __name__=='__main__':unittest.main()
