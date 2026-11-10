import unittest,importlib.util,pathlib
from shapely.geometry import Polygon

def module(name,file):
 s=importlib.util.spec_from_file_location(name,pathlib.Path(__file__).with_name(file));m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
w=module('writer','export-native-copper-gerber.py');r=module('reader','check-copper-gerber.py')
class ExportTests(unittest.TestCase):
 def test_hole_and_tiny_vertex_survive(self):
  p=Polygon([(0,0),(4,0),(4,4),(2,4.0002),(0,4)],holes=[[(1,1),(2,1),(2,2),(1,2)]])
  text=w.encode(p);g,_=r.read_regions(text)
  self.assertIn('Y4000200',text);self.assertEqual(r.compare(p,g)['status'],'passed');self.assertEqual(len(g.interiors),1)
 def test_island_inside_hole(self):
  outer=Polygon([(0,0),(4,0),(4,4),(0,4)],holes=[[(1,1),(3,1),(3,3),(1,3)]])
  island=Polygon([(1.5,1.5),(2,1.5),(2,2),(1.5,2)]);shape=outer.union(island);g,_=r.read_regions(w.encode(shape));self.assertLess(g.symmetric_difference(shape).area,1e-10)
 def test_thin_outer_ring_must_precede_larger_island(self):
  outer=Polygon([(0,0),(10,0),(10,10),(0,10)],holes=[[(.1,.1),(9.9,.1),(9.9,9.9),(.1,9.9)]])
  island=Polygon([(2,2),(8,2),(8,8),(2,8)]);shape=island.union(outer)
  g,_=r.read_regions(w.encode(shape));self.assertLess(g.symmetric_difference(shape).area,1e-10)
if __name__=='__main__':unittest.main()
