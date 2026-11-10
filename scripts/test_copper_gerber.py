import importlib.util,pathlib,unittest
from shapely.geometry import Polygon
spec=importlib.util.spec_from_file_location('copper_gerber',pathlib.Path(__file__).with_name('check-copper-gerber.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
def gerber(points):
 return '%FSLAX46Y46*%\n%MOMM*%\nG36*\n'+''.join('G01X%dY%dD%s*\n'%(round(x*1e6),round(y*1e6),'02' if i==0 else '01') for i,(x,y) in enumerate(points))+'G37*\nM02*\n'
class GerberTests(unittest.TestCase):
 def test_fractured_hole_keeps_void_and_negative_coordinates(self):
  points=[(0,0),(4,0),(4,4),(0,4),(0,2),(1,2),(2,2),(2,1),(1,1),(1,2),(0,2),(0,0)]
  g,r=m.read_regions(gerber([(x,-y) for x,y in points]));self.assertEqual(g.area,15);self.assertEqual(len(g.interiors),1)
 def test_unsupported_or_incomplete_never_silently_pass(self):
  base=gerber([(0,0),(1,0),(1,1),(0,0)])
  for s in [base.replace('M02*',''),base.replace('G01X1000000Y0D01*','G02X1000000Y0D01*'),base.replace('G36*','%LPC*%\nG36*'),base.replace('G37*',''),base.replace('M02*','G01X1Y1D03*\nM02*')]:
   with self.assertRaises(ValueError):m.read_regions(s)
 def test_real_added_copper_fails_even_with_small_total_area(self):
  a=Polygon([(0,0),(1,0),(1,1),(0,1)]);b=Polygon([(0,0),(1.00001,0),(1.00001,1),(0,1)])
  self.assertEqual(m.compare(a,a)['status'],'passed');self.assertEqual(m.compare(a,b)['status'],'failed')
 def test_crossing_or_open_contours_refused(self):
  for pts in [[(0,0),(2,2),(0,2),(2,0),(0,0)],[(0,0),(2,0),(2,2),(0,2)]]:
   with self.assertRaises(ValueError):m.read_regions(gerber(pts))
 def test_outside_region_stroke_is_counted_not_dropped(self):
  base=gerber([(0,0),(1,0),(1,1),(0,0)])
  body='%ADD10C,0.2*%\nG54D10*\nG01X2000000Y0D02*\nG01X3000000Y0D01*\n'
  g,stats=m.read_regions(base.replace('M02*',body+'M02*'))
  self.assertEqual(stats['nonRegionStrokes'],1);self.assertTrue(stats['strokeGeometryApproximate']);self.assertGreater(g.area,0.7)
 def test_stroke_without_aperture_refused(self):
  base=gerber([(0,0),(1,0),(1,1),(0,0)])
  with self.assertRaises(ValueError):m.read_regions(base.replace('M02*','G01X2000000Y0D02*\nG01X3000000Y0D01*\nM02*'))
if __name__=='__main__':unittest.main()
