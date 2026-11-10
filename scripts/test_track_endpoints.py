import importlib.util, pathlib, unittest
s=importlib.util.spec_from_file_location('extractor',pathlib.Path(__file__).with_name('extract-track-endpoints.py'));m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
class EndpointTests(unittest.TestCase):
 def graph(self,segments):return m.extract(m.parse('(kicad_pcb '+segments+')'),dict(id='board',path='fixture',sha256='a'*64))
 def segment(self,a,b,layer='F.Cu',net=1):return '(segment (start '+a+') (end '+b+') (width 0.1) (layer "'+layer+'") (net '+str(net)+'))'
 def test_same_net_disjoint_is_not_connected(self):
  g=self.graph(self.segment('0 0','1 0')+self.segment('2 0','3 0'));self.assertEqual(len(g['nodes']),4);self.assertFalse(g['complete'])
 def test_same_endpoint_different_layers_not_connected(self):
  g=self.graph(self.segment('0 0','1 0')+self.segment('0 0','1 0','B.Cu'));self.assertEqual(len(g['nodes']),4)
 def test_touching_geometry_ignores_net_labels(self):
  g=self.graph(self.segment('0 0','1 0')+self.segment('1.0 -0','2 0',net=2));self.assertEqual(len(g['nodes']),3);self.assertEqual(g['coverage'],[])
 def test_crossing_not_falsely_certified(self):
  g=self.graph(self.segment('0 0','2 2')+self.segment('0 2','2 0'));self.assertFalse(g['complete']);self.assertEqual(len(g['nodes']),4)
 def test_bad_geometry_rejected(self):
  for a,b in [('nan 0','1 0'),('0 0','0 0')]:
   with self.assertRaises(ValueError):self.graph(self.segment(a,b))
 def test_blind_via_does_not_join_bottom(self):
  g=self.graph('(layers (0 "F.Cu" signal) (2 "In1.Cu" signal) (31 "B.Cu" signal)) (via (at 1 2) (size 0.15) (drill 0.07) (layers "F.Cu" "In1.Cu"))')
  self.assertEqual(len(g['edges']),1);self.assertFalse(any(n['id'].startswith('B.Cu') for n in g['nodes']));self.assertFalse(g['complete'])
 def test_unknown_via_layer_is_rejected(self):
  with self.assertRaises(ValueError):self.graph('(layers (0 "F.Cu" signal) (31 "B.Cu" signal)) (via (at 1 2) (layers "F.Cu" "In1.Cu"))')
 def test_arc_midpoint_is_connected(self):
  g=self.graph('(arc (start 0 0) (mid 1 1) (end 2 0) (layer "F.Cu"))')
  self.assertEqual(len(g['edges']),2);self.assertEqual(g['edges'][0]['to'],g['edges'][1]['from'])
 def test_pad_rotation_and_no_net_inference(self):
  g=self.graph('(layers (0 "F.Cu" signal)) (footprint "x" (transform (translate 10 20) (rotate 90) (scale 1 1)) (property "Reference" "U1") (pad "1" smd circle (at -2 3) (size 1 1) (layers "F.Cu")))')
  self.assertTrue(any(n['id']=='F.Cu:13,22' for n in g['nodes']));self.assertFalse(g['complete'])
 def test_copper_hole_does_not_connect_point(self):
  g=self.graph(self.segment('2 2','2 3')+'(zone (filled_polygon (layer "F.Cu") (pts (xy 0 0) (xy 1 0) (xy 1 1) (xy 0 1) (xy 0 0))))')
  self.assertFalse(any(e['id'].startswith('contact-') for e in g['edges']))
 def test_copper_contains_endpoint_even_with_unrelated_net(self):
  g=self.graph(self.segment('0.5 0.5','2 3')+'(zone (filled_polygon (layer "F.Cu") (pts (xy 0 0) (xy 1 0) (xy 1 1) (xy 0 1) (xy 0 0))))')
  self.assertEqual(sum(e['id'].startswith('contact-') for e in g['edges']),1)
 def test_point_in_fractured_copper_hole_is_not_connected(self):
  pts=' '.join('(xy %s %s)'%xy for xy in [(0,0),(4,0),(4,4),(0,4),(0,2),(1,2),(2,2),(2,1),(1,1),(1,2),(0,2),(0,0)])
  g=self.graph(self.segment('1.5 1.5','1.6 1.6')+'(zone (filled_polygon (layer "F.Cu") (pts '+pts+')))')
  self.assertFalse(any(e['id'].startswith('contact-') for e in g['edges']))
if __name__=='__main__':unittest.main()
