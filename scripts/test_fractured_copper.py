import unittest
from shapely.ops import unary_union
from fractured_copper import partition

class CopperPartitionTests(unittest.TestCase):
 def test_fracture_bridge_keeps_hole(self):
  pts=[(0,0),(4,0),(4,4),(0,4),(0,2),(1,2),(2,2),(2,1),(1,1),(1,2),(0,2)]
  pieces,report=partition(pts)
  self.assertEqual(unary_union(pieces).area,15)
  self.assertEqual(report['differenceAreaMm2'],0)
  self.assertTrue(all(not x.interiors and x.is_valid for x in pieces))
 def test_actual_crossing_is_not_silently_fixed(self):
  with self.assertRaisesRegex(ValueError,'changes filled area'):
   partition([(0,0),(2,2),(0,2),(2,0)])
 def test_simple_polygon_remains_identical(self):
  pieces,report=partition([(0,0),(3,0),(3,2),(0,2)])
  self.assertEqual(len(pieces),1)
  self.assertEqual(report['sourceAreaMm2'],6)
if __name__=='__main__':unittest.main()
