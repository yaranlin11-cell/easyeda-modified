import importlib.util,unittest
from pathlib import Path
spec=importlib.util.spec_from_file_location('ses',Path(__file__).with_name('ses-to-apply.py'))
ses=importlib.util.module_from_spec(spec);spec.loader.exec_module(ses)
SOURCE='(session b (routes (resolution mil 1000) (library_out (padstack v (shape (circle TopLayer 26000 0 0)) (shape (circle BottomLayer 26000 0 0)))) (network_out (net "V CC" (wire (path TopLayer 20000 1000 2000 3100 4100)) (via v 3100 4100)))))'
POLICY={'layers':{'TopLayer':1,'BottomLayer':2},'viaHoleMil':13}
class Conversion(unittest.TestCase):
 def test_dimensions_and_net_survive(self):
  x=ses.convert(SOURCE,POLICY)
  self.assertEqual(x[0]['payload'],dict(startX=1,startY=2,endX=3.1,endY=4.1,lineWidth=20,layer=1,net='V CC'))
  self.assertEqual(x[1]['payload'],dict(x=3.1,y=4.1,diameter=26,holeDiameter=13,net='V CC'))
 def test_reject_unsupported(self):
  for text in [SOURCE.replace('mil','mm'),SOURCE.replace('BottomLayer','InnerLayer'),SOURCE.replace('26000 0 0))))','25000 0 0))))')]:
   with self.assertRaises((ValueError,KeyError)):ses.convert(text,POLICY)
 def test_bad_syntax(self):
  with self.assertRaises(ValueError):ses.parse(SOURCE[:-1])
if __name__=='__main__':unittest.main()
